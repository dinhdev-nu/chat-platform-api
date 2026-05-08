package template

import (
	"bytes"
	"embed"
	"fmt"
	"html/template"
)

//go:embed *.html
var templateFS embed.FS

var templates *template.Template

func init() {
	var err error
	templates, err = template.ParseFS(templateFS, "*.html")
	if err != nil {
		panic(fmt.Sprintf("Mailer: failed to parse templates: %v", err))
	}
}

type OTPData struct {
	AppName string
	OTP     string
}

func RenderOTPEmail(data OTPData) (string, error) {
	return render("otp.html", data)
}

func render(name string, data any) (string, error) {
	var buf bytes.Buffer
	if err := templates.ExecuteTemplate(&buf, name, data); err != nil {
		return "", fmt.Errorf("failed to render %s: %w", name, err)
	}
	return buf.String(), nil
}
