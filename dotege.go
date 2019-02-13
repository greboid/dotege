package main

import (
	"fmt"
	"github.com/docker/docker/client"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"
)

type Container struct {
	Id     string
	Name   string
	Labels map[string]string
}

type LabelConfig struct {
	Hostnames string
}

type Hostname struct {
	Name         string
	Alternatives map[string]bool
	Containers   []Container
}

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
	done := monitorSignals()
	containerChan := make(chan Container, 1)
	expiryChan := make(chan string, 1)
	labelConfig := LabelConfig{
		Hostnames: "com.chameth.vhost",
	}

	cli, err := client.NewEnvClient()
	if err != nil {
		panic(err)
	}

	templateGenerator := NewTemplateGenerator()
	templateGenerator.AddTemplate(TemplateConfig{
		Source:      "./templates/domains.txt.tpl",
		Destination: "domains.txt",
	})

	monitor := NewContainerMonitor(cli, containerChan, expiryChan)
	go monitor.Monitor()

	go func() {
		containers := make(map[string]Container)
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

func getHostnames(containers map[string]Container, config LabelConfig) (hostnames map[string]Hostname) {
	hostnames = make(map[string]Hostname)
	for _, container := range containers {
		if label, ok := container.Labels[config.Hostnames]; ok {
			names := strings.Split(strings.Replace(label, ",", " ", -1), " ")
			if hostname, ok := hostnames[names[0]]; ok {
				hostname.Containers = append(hostname.Containers, container)
			} else {
				hostnames[names[0]] = Hostname{
					Name:         names[0],
					Alternatives: make(map[string]bool),
					Containers:   []Container{container},
				}
			}
			addAlternatives(hostnames[names[0]], names[1:])
		}
	}
	return
}

func addAlternatives(hostname Hostname, alternatives []string) {
	for _, alternative := range alternatives {
		hostname.Alternatives[alternative] = true
	}
}
