{{ range .Hostnames }}
    {{- .Name }}{{ range $san, $true := .Alternatives }} {{ $san }}{{ end }}
{{ end }}
