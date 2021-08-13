{{ define "page" }}
<!doctype html>

<html lang="en">
    <head>
        <meta charset="UTF-8">
        <title>API Reference Docs</title>
        <link rel="stylesheet" href="https://maxcdn.bootstrapcdn.com/bootstrap/4.3.1/css/bootstrap.min.css">
        <link rel="stylesheet" href="css/k8s-api-ref-style.css" type="text/css">
    </head>
    <body>
        <div id="sidebar-wrapper" class="side-nav side-bar-nav">
            {{ with .packages}}
            <ul>
                <li class="nav-level-1 strong-nav"><strong>Packages</strong></li>
            </ul>
            <ul>
                {{ range . }}
                <li class="nav-level-1">
                    <a class="nav-item" href="#{{- packageAnchorID . -}}">{{ packageDisplayName . }}</a>
                    <ul>
                    {{- range (visibleTypes (sortedTypes .Types)) -}}
                        {{ if isExportedType . -}}
                        <li class="nav-level-2 strong-nav">
                            <a class="nav-item" href="{{ linkForType . }}">{{ typeDisplayName . }}</a>
                        </li>
                        {{- end }}
                    {{- end -}}
                    </ul>
                </li>
                {{ end }}
            </ul>
            {{ end}}
        </div>
        <div id="page-content-wrapper" class="body-content container">
            {{ template "packages" .  }}

            <div class="text-right">
                <div>
                Generated using <a href="https://github.com/company/project"><code>crd-docs-generator</code></a>
                            {{ with .gitCommit }} on git commit <code>{{ . }}</code>{{end}}.
                </div>
            </div>
        </div>
    </body>
</html>
{{ end }}
