package main

import (
	"fmt"
	"github.com/docker/distribution/context"
	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/client"
	"os"
	"os/signal"
	"sort"
	"strings"
	"syscall"
	"text/template"
	"time"
)

type Container struct {
	Id     string
	Name   string
	Labels map[string]string
}

type Context struct {
	Containers map[string]Container
}

var funcMap = template.FuncMap{
	"replace": func(from, to, input string) string { return strings.Replace(input, from, to, -1) },
	"split":   func(sep, input string) []string { return strings.Split(input, sep) },
	"join":    func(sep string, input []string) string { return strings.Join(input, sep) },
	"sortlines": func(input string) string {
		lines := strings.Split(input, "\n")
		sort.Strings(lines)
		return strings.Join(lines, "\n")
	},
}

func main() {
	sigs := make(chan os.Signal, 1)
	done := make(chan bool, 1)
	containerChan := make(chan Container, 1)
	expiryChan := make(chan string, 1)

	signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM)

	cli, err := client.NewEnvClient()
	if err != nil {
		panic(err)
	}

	go func() {
		sig := <-sigs
		fmt.Printf("Received %s signal\n", sig)
		err := cli.Close()
		if err != nil {
			panic(err)
		}
		done <- true
	}()

	go func() {
		const deletionTime = 10 * time.Second

		expiryTimes := make(map[string]time.Time)
		expiryTimer := time.NewTimer(time.Hour)
		nextExpiry := time.Now()
		expiryTimer.Stop()

		args := filters.NewArgs()
		args.Add("type", "container")
		args.Add("event", "create")
		args.Add("event", "destroy")
		eventsChan, errChan := cli.Events(context.Background(), types.EventsOptions{Filters: args})

		containers, err := cli.ContainerList(context.Background(), types.ContainerListOptions{})
		if err != nil {
			panic(err)
		}

		for _, container := range containers {
			containerChan <- Container{
				Id:     container.ID,
				Name:   container.Names[0][1:],
				Labels: container.Labels,
			}
		}

		for {
			select {
			case event := <-eventsChan:
				if event.Action == "create" {
					container, err := cli.ContainerInspect(context.Background(), event.Actor.ID)
					if err != nil {
						panic(err)
					}
					containerChan <- Container{
						Id:     container.ID,
						Name:   container.Name[1:],
						Labels: container.Config.Labels,
					}
				} else {
					now := time.Now()
					expiryTime := now.Add(deletionTime)
					expiryTimes[event.Actor.Attributes["name"]] = expiryTime
					fmt.Printf("Scheduling expiry timer for %s\n", event.Actor.Attributes["name"])
					if nextExpiry.Before(now) || nextExpiry.After(expiryTime) {
						fmt.Printf("Starting expiry timer with default duration\n")
						expiryTimer.Reset(deletionTime + 1*time.Second)
						nextExpiry = expiryTime
					}
				}

			case <-expiryTimer.C:
				now := time.Now()
				next := 0 * time.Second

				for name, expiryTime := range expiryTimes {
					if expiryTime.Before(now) {
						fmt.Printf("Expiring %s\n", name)
						delete(expiryTimes, name)
						expiryChan <- name
					} else if next == 0 || expiryTime.Sub(now) < next {
						next = expiryTime.Sub(now)
					}
				}

				if next > 0 {
					fmt.Printf("Starting expiry timer with duration %s\n", next)
					expiryTimer.Reset(next + 1*time.Second)
					nextExpiry = now.Add(next)
				}

			case err := <-errChan:
				panic(err)
			}
		}
	}()

	go func() {
		var templates []*template.Template
		containers := make(map[string]Container)
		timer := time.NewTimer(time.Hour)
		timer.Stop()

		tmpl, err := template.New("domains.txt.tpl").Funcs(funcMap).ParseFiles("./templates/domains.txt.tpl")
		if err != nil {
			panic(err)
		}
		templates = append(templates, tmpl)

		tmpl, err = template.New("haproxy.cfg.tpl").Funcs(funcMap).ParseFiles("./templates/haproxy.cfg.tpl")
		if err != nil {
			panic(err)
		}
		templates = append(templates, tmpl)

		for {
			select {
			case container := <-containerChan:
				containers[container.Name] = container
				timer.Reset(100 * time.Millisecond)
			case name := <-expiryChan:
				delete(containers, name)
				timer.Reset(100 * time.Millisecond)
			case <-timer.C:
				for _, tmpl := range templates {
					fmt.Printf("--- Writing %s ---\n", tmpl.Name())
					err = tmpl.Execute(os.Stdout, Context{Containers: containers})
					fmt.Printf("--- / writing %s ---\n", tmpl.Name())
					if err != nil {
						panic(err)
					}
				}
			}
		}
	}()

	<-done
}
