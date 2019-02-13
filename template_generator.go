package main

import (
	"fmt"
	"io/ioutil"
	"path"
	"sort"
	"strings"
	"text/template"
)

type Context struct {
	Containers map[string]Container
}

type TemplateConfig struct {
	Source      string
	Destination string
}

type Template struct {
	config   TemplateConfig
	content  string
	template *template.Template
}

type TemplateGenerator struct {
	templates []*Template
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

func NewTemplateGenerator() *TemplateGenerator {
	return &TemplateGenerator{}
}

func (t *TemplateGenerator) AddTemplate(config TemplateConfig) {
	tmpl, err := template.New(path.Base(config.Source)).Funcs(funcMap).ParseFiles(config.Source)
	if err != nil {
		panic(err)
	}

	buf, _ := ioutil.ReadFile(config.Destination)
	t.templates = append(t.templates, &Template{
		config:   config,
		content:  string(buf),
		template: tmpl,
	})
}

func (t *TemplateGenerator) Generate(context Context) {
	for _, tmpl := range t.templates {
		fmt.Printf("Checking %s\n", tmpl.config.Source)
		builder := &strings.Builder{}
		err := tmpl.template.Execute(builder, context)
		if err != nil {
			panic(err)
		}
		if tmpl.content != builder.String() {
			fmt.Printf("--- %s updated, writing to %s ---\n", tmpl.config.Source, tmpl.config.Destination)
			fmt.Printf("%s", builder.String())
			fmt.Printf("--- / writing %s ---\n", tmpl.config.Destination)
			tmpl.content = builder.String()
			err = ioutil.WriteFile(tmpl.config.Destination, []byte(builder.String()), 0666)
			if err != nil {
				panic(err)
			}
		}
	}
}
