package main

import (
	"fmt"
	"github.com/docker/docker/client"
	"os"
	"os/signal"
	"syscall"
	"time"
)

type Container struct {
	Id     string
	Name   string
	Labels map[string]string
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
				templateGenerator.Generate(Context{Containers: containers})
			}
		}
	}()

	<-done

	err = cli.Close()
	if err != nil {
		panic(err)
	}
}
