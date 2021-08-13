{{ define "page" }}
    Packages:
    {{ with .packages}}
        {{ range . }}
        * {{ packageDisplayName . }}
            {{- range (visibleTypes (sortedTypes .Types)) -}}
                {{ if isExportedType . -}}
                    * [{{ typeDisplayName . }}]({{ linkForType . }})
                {{- end }}
            {{- end -}}
        {{ end }}
    {{ end}}
    {{ template "packages" .  }}


    Generated using <a href="https://github.com/company/project"><code>crd-docs-generator</code></a>
                {{ with .gitCommit }} on git commit <code>{{ . }}</code>{{end}}.
{{ end }}
