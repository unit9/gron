package main

import (
	"flag"
	"fmt"
	"io/ioutil"
	"os"
	"os/exec"
	"sync"
	"time"

	"go.uber.org/zap"
	"gopkg.in/yaml.v2"
)

var debug = flag.Bool("d", false, "enable debug logging")

var rawlog *zap.Logger
var log *zap.SugaredLogger

type Cron struct {
	CronJobs []*CronJob `yaml:"cron"`
}

type CronJob struct {
	Description string        `yaml:"description"`
	Command     string        `yaml:"command"`
	Minute      *int          `yaml:"minute"`
	Hour        *int          `yaml:"hour"`
	Day         *int          `yaml:"day"`
	Weekday     *time.Weekday `yaml:"weekday"`
	Lock        bool          `yaml:"lock"`

	m sync.Mutex
	x bool // must hold m to read/write
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
	cmd := exec.Command("/bin/sh", "-c", j.Command)
	out, err := cmd.CombinedOutput()
	log.Infow("completed", "job", j, "out", string(out), "err", err)
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

func InitLogging(debug bool) {
	if debug {
		rawlog, _ = zap.NewDevelopment()
	} else {
		rawlog, _ = zap.NewProduction()
	}
	log = rawlog.Sugar()
}

func main() {
	flag.Usage = Usage
	flag.Parse()
	jobs := []*CronJob{}
	InitLogging(*debug)
	for _, arg := range flag.Args() {
		c, err := LoadCron(arg)
		if err != nil {
			log.Fatalw("load", "error", err)
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
			if j.IsItTime() {
				j.Run()
			}
		}
		WaitUntilNextMinute()
	}
}