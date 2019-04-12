package main

import (
	"io/ioutil"
	"path"
	"sort"
	"strings"
	"text/template"
)

type Context struct {
	Containers map[string]*Container
	Hostnames  map[string]*Hostname
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
	logger.Infof("Registered template from %s, writing to %s", config.Source, config.Destination)
	tmpl, err := template.New(path.Base(config.Source)).Funcs(funcMap).ParseFiles(config.Source)
	if err != nil {
		logger.Fatal("Unable to parse template", err)
	}

	buf, _ := ioutil.ReadFile(config.Destination)
	t.templates = append(t.templates, &Template{
		config:   config,
		content:  string(buf),
		template: tmpl,
	})
}

func (t *TemplateGenerator) Generate(context Context) (updated bool) {
	for _, tmpl := range t.templates {
		logger.Debugf("Checking for updates to %s", tmpl.config.Source)
		builder := &strings.Builder{}
		err := tmpl.template.Execute(builder, context)
		if err != nil {
			panic(err)
		}
		if tmpl.content != builder.String() {
			updated = true
			logger.Infof("Writing updated template to %s", tmpl.config.Destination)
			tmpl.content = builder.String()
			err = ioutil.WriteFile(tmpl.config.Destination, []byte(builder.String()), 0666)
			if err != nil {
				logger.Fatal("Unable to write template", err)
			}
		} else {
			logger.Debugf("Not writing template to %s as content is the same", tmpl.config.Destination)
		}
	}
	return
}
