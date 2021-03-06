package command

import (
	"fmt"
	"log"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/czxichen/command/watchdog"
	conf "github.com/dlintw/goconf"
)

var Watchdog = &Command{
	UsageLine: `watchdog -config watch.ini`,
	Run:       watchdogrun,
	Short:     "进程守护",
	Long: `用来监控进程,可以带依赖模式监控
	watchdog -config watch.ini
`,
}
var logpath, configFile string

func init() {
	Watchdog.Flag.StringVar(&logpath, "log_path", "", "Specify log path")
	Watchdog.Flag.StringVar(&configFile, "config", "watchdog.ini", "Watchdog configuration file")
}

func watchdogrun(cmd *Command, args []string) bool {
	if logpath == "" {
		logpath = "watchdog.log"
	}
	logFile, err := os.Create(logpath)
	if err != nil {
		log.Fatalf("Create log file error:%s\n", err.Error())
	}
	defer logFile.Close()
	log.SetOutput(logFile)

	cfg, err := conf.ReadConfigFile(configFile)
	if err != nil {
		log.Fatalf("Failed to read config file %q: %v", configFile, err)
	}

	fido := watchdog.NewWatchdog()
	shutdownHandler(fido)
	for _, name := range cfg.GetSections() {
		if name == "default" {
			continue
		}
		binary := svcOpt(cfg, name, "binary", true)
		args := svcOpt(cfg, name, "args", false)

		svc, err := fido.AddService(name, binary)
		if err != nil {
			log.Fatalf("Failed to add service %q: %v", name, err)
		}
		svc.AddArgs(args)
		if dep := svcOpt(cfg, name, "dependency", false); dep != "" {
			svc.AddDependency(dep)
		}
		if opt := svcOpt(cfg, name, "priority", false); opt != "" {
			prio, err := strconv.Atoi(opt)
			if err != nil {
				log.Fatalf("Service %s has invalid priority %q: %v", name, opt, err)
			}
			if err := svc.SetPriority(prio); err != nil {
				log.Fatalf("Failed to set priority for service %s: %v", name, err)
			}
		}
		if opt := svcOpt(cfg, name, "term_timeout", false); opt != "" {
			tt, err := time.ParseDuration(opt)
			if err != nil {
				log.Fatalf("Service %s has invalid term_timeout %q: %v", name, opt, err)
			}
			svc.SetTermTimeout(tt)
		}

		if user := svcOpt(cfg, name, "user", false); user != "" {
			if err := svc.SetUser(user); err != nil {
				log.Fatalf("Failed to set user for service %s: %v", name, err)
			}
		}
	}
	fido.Walk()
	return true
}

func cfgOpt(cfg *conf.ConfigFile, section, option string) string {
	if !cfg.HasOption(section, option) {
		return ""
	}
	s, err := cfg.GetString(section, option)
	if err != nil {
		log.Fatalf("Failed to get %s for %s: %v", option, section, err)
	}
	return s
}

func svcOpt(cfg *conf.ConfigFile, service, option string, required bool) string {
	opt := cfgOpt(cfg, service, option)
	if opt == "" && required {
		log.Fatalf("Service %s has missing %s option", service, option)
	}
	return opt
}

var signalNames = map[syscall.Signal]string{
	syscall.SIGINT:  "SIGINT",
	syscall.SIGQUIT: "SIGQUIT",
	syscall.SIGTERM: "SIGTERM",
}

func signalName(s syscall.Signal) string {
	if name, ok := signalNames[s]; ok {
		return name
	}
	return fmt.Sprintf("SIG %d", s)
}

type Shutdowner interface {
	Shutdown()
}

func shutdownHandler(server Shutdowner) {
	sigc := make(chan os.Signal, 3)
	signal.Notify(sigc, syscall.SIGINT, syscall.SIGQUIT, syscall.SIGTERM)
	go func() {
		for s := range sigc {
			name := s.String()
			if sig, ok := s.(syscall.Signal); ok {
				name = signalName(sig)
			}
			log.Printf("Received %v, initiating shutdown...", name)
			server.Shutdown()
		}
	}()
}
