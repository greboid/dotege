package main

import (
	"io/ioutil"
	"path"
	"sort"
	"strings"
	"text/template"
)

var templateFuncs = template.FuncMap{
	"replace": func(from, to, input string) string { return strings.Replace(input, from, to, -1) },
	"split":   func(sep, input string) []string { return strings.Split(input, sep) },
	"join":    func(sep string, input []string) string { return strings.Join(input, sep) },
	"sortlines": func(input string) string {
		lines := strings.Split(input, "\n")
		sort.Strings(lines)
		return strings.Join(lines, "\n")
	},
}

type Template struct {
	source      string
	destination string
	content     string
	template    *template.Template
}

func CreateTemplate(source, destination string) *Template {
	loggers.main.Infof("Registered template from %s, writing to %s", source, destination)
	tmpl, err := template.New(path.Base(source)).Funcs(templateFuncs).ParseFiles(source)
	if err != nil {
		loggers.main.Fatal("Unable to parse template", err)
	}

	buf, _ := ioutil.ReadFile(destination)
	return &Template{
		source:      source,
		destination: destination,
		content:     string(buf),
		template:    tmpl,
	}
}

type Templates []*Template

func (t Templates) Generate(context interface{}) (updated bool) {
	for _, tmpl := range t {
		loggers.main.Debugf("Checking for updates to %s", tmpl.source)
		builder := &strings.Builder{}
		err := tmpl.template.Execute(builder, context)
		if err != nil {
			panic(err)
		}
		if tmpl.content != builder.String() {
			updated = true
			loggers.main.Infof("Writing updated template to %s", tmpl.destination)
			tmpl.content = builder.String()
			err = ioutil.WriteFile(tmpl.destination, []byte(builder.String()), 0666)
			if err != nil {
				loggers.main.Fatal("Unable to write template", err)
			}
		} else {
			loggers.main.Debugf("Not writing template to %s as content is the same", tmpl.destination)
		}
	}
	return
}
