package main

import (
	"github.com/csmith/dotege/model"
	"go.uber.org/zap"
	"io/ioutil"
	"path"
	"sort"
	"strings"
	"text/template"
)

type Context struct {
	Containers map[string]model.Container
	Hostnames  map[string]model.Hostname
}

type Template struct {
	config   model.TemplateConfig
	content  string
	template *template.Template
}

type TemplateGenerator struct {
	logger    *zap.SugaredLogger
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

func NewTemplateGenerator(logger *zap.SugaredLogger) *TemplateGenerator {
	return &TemplateGenerator{
		logger: logger,
	}
}

func (t *TemplateGenerator) AddTemplate(config model.TemplateConfig) {
	t.logger.Infof("Adding template from %s, writing to %s", config.Source, config.Destination)
	tmpl, err := template.New(path.Base(config.Source)).Funcs(funcMap).ParseFiles(config.Source)
	if err != nil {
		t.logger.Fatal("Unable to parse template", err)
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
		t.logger.Debugf("Checking for updates to %s", tmpl.config.Source)
		builder := &strings.Builder{}
		err := tmpl.template.Execute(builder, context)
		if err != nil {
			panic(err)
		}
		if tmpl.content != builder.String() {
			t.logger.Debugf("%s has been updated, writing to %s", tmpl.config.Source, tmpl.config.Destination)
			t.logger.Debug(builder.String())
			tmpl.content = builder.String()
			err = ioutil.WriteFile(tmpl.config.Destination, []byte(builder.String()), 0666)
			if err != nil {
				t.logger.Fatal("Unable to write template", err)
			}
		}
	}
}
