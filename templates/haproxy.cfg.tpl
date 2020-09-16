global
    ssl-default-bind-ciphers ECDHE-ECDSA-AES256-GCM-SHA384:ECDHE-RSA-AES256-GCM-SHA384:ECDHE-ECDSA-CHACHA20-POLY1305:ECDHE-RSA-CHACHA20-POLY1305:ECDHE-ECDSA-AES128-GCM-SHA256:ECDHE-RSA-AES128-GCM-SHA256:ECDHE-ECDSA-AES256-SHA384:ECDHE-RSA-AES256-SHA384:ECDHE-ECDSA-AES128-SHA256:ECDHE-RSA-AES128-SHA256
    ssl-default-bind-options no-sslv3 no-tlsv10 no-tlsv11 no-tls-tickets
    ssl-default-server-ciphers ECDHE-ECDSA-AES256-GCM-SHA384:ECDHE-RSA-AES256-GCM-SHA384:ECDHE-ECDSA-CHACHA20-POLY1305:ECDHE-RSA-CHACHA20-POLY1305:ECDHE-ECDSA-AES128-GCM-SHA256:ECDHE-RSA-AES128-GCM-SHA256:ECDHE-ECDSA-AES256-SHA384:ECDHE-RSA-AES256-SHA384:ECDHE-ECDSA-AES128-SHA256:ECDHE-RSA-AES128-SHA256
    ssl-default-server-options no-sslv3 no-tlsv10 no-tlsv11 no-tls-tickets

resolvers docker_resolver
    nameserver dns 127.0.0.11:53

defaults
    log global
    mode    http
    timeout connect 5000
    timeout client 30000
    timeout server 30000
    compression algo gzip
    compression type text/plain text/css application/json application/javascript application/x-javascript text/xml application/xml application/xml+rss text/javascript
    default-server init-addr last,libc,none check resolvers docker_resolver

{{- if len .Groups | lt 0 }}

userlist dotege
    {{- range .Groups }}
    group {{.}}
    {{- end -}}

    {{- range .Users }}
    user {{.Name}} password {{.Password}}
    {{- if len .Groups | lt 0 }} groups {{ .Groups | join "," }}{{ end }}
    {{- end }}
{{- end }}

frontend main
    mode    http
    bind    :::443 v4v6 ssl strict-sni alpn h2,http/1.1 crt /certs/
    bind    :::80 v4v6
    http-request set-header X-Forwarded-For %[src]
    http-request set-header X-Forwarded-Proto https if { ssl_fc }
    redirect scheme https code 301 if !{ ssl_fc }
    http-response set-header Strict-Transport-Security max-age=15768000
{{- range .Hostnames }}
    use_backend {{ .Name | replace "." "_" }} if { hdr(host) -i {{ .Name }}
        {{- range .Alternatives }} || hdr(host) -i {{ . }} {{- end }} }
{{- end -}}

{{ range .Hostnames }}

backend {{ .Name | replace "." "_" }}
    mode http
    {{- range .Containers }}
        {{- if .ShouldProxy }}
    server server1 {{ .Name }}:{{ .Port }}
        {{- end -}}
    {{- end -}}
    {{- range $k, $v := .Headers }}
    http-response set-header {{ $k }} "{{ $v | replace "\"" "\\\"" }}"
    {{- end -}}
    {{- if .RequiresAuth }}
    acl authed_{{ .Name | replace "." "_" }} http_auth(dotege) {{ .AuthGroup }}
    http-request auth if !authed_{{ .Name | replace "." "_" }}
    {{- end -}}
{{ end }}
