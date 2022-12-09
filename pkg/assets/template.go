package assets

import (
	"bytes"
	"fmt"
	"strings"
	"text/template"

	"sigs.k8s.io/yaml"
)

var TemplateFuncs template.FuncMap = template.FuncMap{
	"toYAML":  marshalYAML,
	"indent":  indent,
	"nindent": nindent,
}

func marshalYAML(v any) (string, error) {
	bytes, err := yaml.Marshal(v)
	return strings.TrimSpace(string(bytes)), err
}

func indent(spaceCount int, s string) string {
	spaces := strings.Repeat(" ", spaceCount)
	return spaces + strings.Replace(s, "\n", "\n"+spaces, -1)
}

func nindent(spaceCount int, s string) string {
	return "\n" + indent(spaceCount, s)
}

func RenderTemplate(tmpl *template.Template, inputs any) ([]byte, error) {
	// We always want correctness. (Accidentally missing a key might have side effects.)
	tmpl.Option("missingkey=error")

	var buf bytes.Buffer
	err := tmpl.Execute(&buf, inputs)
	if err != nil {
		return nil, fmt.Errorf("can't execute template %q: %w", tmpl.Name(), err)
	}

	return buf.Bytes(), nil
}
