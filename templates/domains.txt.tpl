{{ range .Containers }}
    {{- with index .Labels "com.chameth.vhost" -}}
        {{ . | replace "," " " }}{{ "\n" }}
    {{- end -}}
{{ end }}