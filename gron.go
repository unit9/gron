package main

import (
	"context"
	"flag"
	"fmt"
	"io/ioutil"
	"os"
	"os/exec"
	"sync"
	"time"

	"github.com/getsentry/raven-go"
	"go.uber.org/zap"
	"gopkg.in/yaml.v2"
)

const VERSION = "v0.2"

var debug = flag.Bool("d", false, "enable debug logging")
var version = flag.Bool("v", false, "show version and exit")

var rawlog *zap.Logger
var log *zap.SugaredLogger
var sentry *raven.Client

type Cron struct {
	CronJobs []*CronJob     `yaml:"cron"`
	Report   *ReportOptions `yaml:"report"`
}

type CronJob struct {
	// What are we running anyway?
	Description string `yaml:"description"`
	Command     string `yaml:"command"`
	Pwd         string `yaml:"pwd"`

	// When? How often?
	Minute  *int          `yaml:"minute"`
	Hour    *int          `yaml:"hour"`
	Day     *int          `yaml:"day"`
	Weekday *time.Weekday `yaml:"weekday"`

	// How long do we allow it to run?
	Timeout *int `yaml:"timeout"`

	// Do we prevent it from running again if it's already running?
	Lock bool `yaml:"lock"`

	// Private locking stuff
	m sync.Mutex
	x bool // must hold m to read/write
}

type ReportOptions struct {
	SentryDSN string `yaml:"SENTRY_DSN"`
}

func Usage() {
	fmt.Fprintf(os.Stderr, "Usage:\n")
	fmt.Fprintf(os.Stderr, "    %s [flags] cron.yml [cron2.yml ...]\n",
		os.Args[0])
	fmt.Fprintf(os.Stderr, "Flags:\n")
	flag.PrintDefaults()
}

func LoadCron(fpath string) (*Cron, error) {
	var c Cron
	data, err := ioutil.ReadFile(fpath)
	if err != nil {
		return nil, err
	}
	err = yaml.Unmarshal([]byte(data), &c)
	if err != nil {
		return nil, err
	}
	return &c, nil
}

func (j *CronJob) IsItTime() bool {
	now := time.Now()
	if j.Minute != nil && *j.Minute != now.Minute() {
		return false
	}
	if j.Hour != nil && *j.Hour != now.Hour() {
		return false
	}
	if j.Day != nil && *j.Day != now.Day() {
		return false
	}
	if j.Weekday != nil && *j.Weekday != now.Weekday() {
		return false
	}
	return true
}

func (j *CronJob) innerRun() {
	log.Infow("running", "job", j)
	var cmd *exec.Cmd
	if j.Timeout == nil {
		cmd = exec.Command("/bin/sh", "-c", j.Command)
	} else {
		timeout := time.Second * time.Duration(*j.Timeout)
		ctx, cancel := context.WithTimeout(context.Background(), timeout)
		defer cancel()
		cmd = exec.CommandContext(ctx, "/bin/sh", "-c", j.Command)
	}
	cmd.Dir = j.Pwd
	wp, err := cmd.StdinPipe()
	if err != nil {
		log.Fatalw("stdinpipe", "error", err)
	}
	wp.Close()
	out, err := cmd.CombinedOutput()
	strOut := string(out)
	log.Infow("completed", "job", j, "out", strOut, "err", err)
	if err != nil {
		packet := raven.NewPacket(
			fmt.Sprintf("Job failed: %s: %s", j.Description, err.Error()),
		)
		packet.Extra["err"] = err.Error()
		packet.Extra["pwd"] = j.Pwd
		packet.Extra["command"] = j.Command
		packet.Extra["description"] = j.Description
		packet.Extra["out"] = strOut
		sentry.Capture(packet, nil)
	}
}

func (j *CronJob) Run() {
	log.Debugw("considering", "job", j)
	if !j.Lock {
		go j.innerRun()
		return
	}
	j.m.Lock()
	defer j.m.Unlock()
	if j.x {
		log.Debugw("already running", "job", j)
		return
	}
	j.x = true
	go func() {
		defer func() {
			j.m.Lock()
			defer j.m.Unlock()
			j.x = false
		}()
		j.innerRun()
	}()
}

func (j *CronJob) Fix() {
	if j.Command == "" {
		panic("No command specified: " + j.Description)
	}
}

func WaitUntilNextMinute() {
	now := time.Now()
	then := now.Add(time.Duration(time.Minute)).Truncate(time.Minute)
	time.Sleep(then.Sub(now))
}

func InitLogging() {
	var err error
	if *debug {
		rawlog, err = zap.NewDevelopment()
	} else {
		rawlog, err = zap.NewProduction()
	}
	if err != nil {
		panic(err)
	}
	log = rawlog.Sugar()
	sentry, err = raven.New(os.Getenv("SENTRY_DSN"))
	if err != nil {
		panic(err)
	}
}

func main() {
	flag.Usage = Usage
	flag.Parse()
	if *version {
		fmt.Printf("gron %s\n", VERSION)
		os.Exit(0)
	}
	jobs := []*CronJob{}
	InitLogging()
	for _, arg := range flag.Args() {
		c, err := LoadCron(arg)
		if err != nil {
			log.Fatalw("load", "error", err)
		}
		if c.Report != nil && c.Report.SentryDSN != "" {
			sentry.SetDSN(c.Report.SentryDSN)
		}
		jobs = append(jobs, c.CronJobs...)
	}
	log.Infow("hello", "jobs", jobs)
	for _, j := range jobs {
		j.Fix()
	}
	for {
		log.Debugw("tick", "now", time.Now())
		for _, j := range jobs {
			sentry.CapturePanic(func() {
				if j.IsItTime() {
					j.Run()
				}
			}, nil)
		}
		WaitUntilNextMinute()
	}
}
