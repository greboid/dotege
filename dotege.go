package main

import (
	"fmt"
	"github.com/csmith/dotege/certs"
	"github.com/csmith/dotege/docker"
	"github.com/csmith/dotege/model"
	"github.com/docker/docker/client"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"
)

func monitorSignals() <-chan bool {
	signals := make(chan os.Signal, 1)
	done := make(chan bool, 1)

	signal.Notify(signals, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		sig := <-signals
		fmt.Printf("Received %s signal\n", sig)
		done <- true
	}()

	return done
}

func main() {
	config := zap.NewDevelopmentConfig()
	config.DisableCaller = true
	config.DisableStacktrace = true
	config.EncoderConfig.EncodeLevel = zapcore.CapitalColorLevelEncoder
	config.OutputPaths = []string{"stdout"}
	config.ErrorOutputPaths = []string{"stdout"}
	logger, _ := config.Build()
	sugar := logger.Sugar()
	sugar.Info("Dotege is starting")

	done := monitorSignals()
	containerChan := make(chan model.Container, 1)
	expiryChan := make(chan string, 1)
	labelConfig := model.LabelConfig{
		Hostnames: "com.chameth.vhost",
	}

	cli, err := client.NewEnvClient()
	if err != nil {
		panic(err)
	}

	certMonitor := certs.NewCertificateManager(sugar)
	certMonitor.AddDirectory("/certs/certs")

	templateGenerator := NewTemplateGenerator(sugar)
	templateGenerator.AddTemplate(model.TemplateConfig{
		Source:      "./templates/domains.txt.tpl",
		Destination: "domains.txt",
	})
	templateGenerator.AddTemplate(model.TemplateConfig{
		Source:      "./templates/haproxy.cfg.tpl",
		Destination: "haproxy.cfg",
	})

	monitor := docker.NewContainerMonitor(sugar, cli, containerChan, expiryChan)
	go monitor.Monitor()

	go func() {
		containers := make(map[string]model.Container)
		timer := time.NewTimer(time.Hour)
		timer.Stop()

		for {
			select {
			case container := <-containerChan:
				containers[container.Name] = container
				timer.Reset(100 * time.Millisecond)
			case name := <-expiryChan:
				delete(containers, name)
				timer.Reset(100 * time.Millisecond)
			case <-timer.C:
				templateGenerator.Generate(Context{
					Containers: containers,
					Hostnames:  getHostnames(containers, labelConfig),
				})
			}
		}
	}()

	<-done

	err = cli.Close()
	if err != nil {
		panic(err)
	}
}

func getHostnames(containers map[string]model.Container, config model.LabelConfig) (hostnames map[string]*model.Hostname) {
	hostnames = make(map[string]*model.Hostname)
	for _, container := range containers {
		if label, ok := container.Labels[config.Hostnames]; ok {
			names := strings.Split(strings.Replace(label, ",", " ", -1), " ")
			if hostname, ok := hostnames[names[0]]; ok {
				hostname.Containers = append(hostname.Containers, container)
			} else {
				hostnames[names[0]] = &model.Hostname{
					Name:         names[0],
					Alternatives: make(map[string]bool),
					Containers:   []model.Container{container},
				}
			}
			addAlternatives(hostnames[names[0]], names[1:])
		}
	}
	return
}

func addAlternatives(hostname *model.Hostname, alternatives []string) {
	for _, alternative := range alternatives {
		hostname.Alternatives[alternative] = true
	}
}
