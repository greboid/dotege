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
    timeout client 5000
    timeout server 5000
    compression algo gzip
    compression type text/plain text/css application/json application/javascript application/x-javascript text/xml application/xml application/xml+rss text/javascript
    default-server init-addr last,libc,none check resolvers docker_resolver

frontend main
    mode    http
    bind    :443 ssl strict-sni alpn h2,http/1.1 crt /certs/
    bind    :80
    http-request set-header X-Forwarded-For %[src]
    http-request set-header X-Forwarded-Proto https if { ssl_fc }
    redirect scheme https code 301 if !{ ssl_fc }
    http-response set-header Strict-Transport-Security max-age=15768000
{{- range .Hostnames }}
    use_backend {{ .Name | replace "." "_" }} if { hdr(host) -i {{ .Name }}
        {{- range $san, $_ := .Alternatives }} || hdr(host) -i {{ $san }} {{- end }} }
{{- end -}}

{{ range .Hostnames }}

backend {{ .Name | replace "." "_" }}
    mode http
    {{- range .Containers }}
        {{- if index .Labels "com.chameth.proxy" }}
    server server1 {{ .Name }}:{{ index .Labels "com.chameth.proxy" }}
        {{- end -}}
    {{- end -}}
    {{- if .RequiresAuth }}
    acl authed_{{ .Name | replace "." "_" }} http_auth({{ .AuthGroup }})
    http-request auth if !authed_{{ .Name | replace "." "_" }}
    {{- end -}}
{{ end }}
