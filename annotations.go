package main

import (
	"os"

	md "github.com/JohannesKaufmann/html-to-markdown"
	"github.com/JohannesKaufmann/html-to-markdown/plugin"
	"github.com/charmbracelet/glamour"
	buildkite "github.com/kevinburke/buildkite/lib"
	"golang.org/x/term"
)

func getTerminalWidth() int {
	width, _, err := term.GetSize(int(os.Stdout.Fd()))
	if err != nil || width <= 0 {
		return 80 // fallback width if we can't determine the actual width
	}
	return width
}

func getANSIAnnotations(annotations buildkite.AnnotationResponse) ([]string, error) {
	width := getTerminalWidth()
	if width > 120 {
		width = 120
	}
	renderer, err := glamour.NewTermRenderer(
		glamour.WithAutoStyle(),
		glamour.WithWordWrap(width),
	)
	if err != nil {
		return nil, err
	}
	var messages []string
	converter := md.NewConverter("", true, nil)
	converter.Use(plugin.Table())
	for _, annotation := range annotations {
		content, err := converter.ConvertString(annotation.BodyHTML)
		if err != nil {
			return nil, err
		}
		out, err := renderer.Render(content)
		if err != nil {
			return nil, err
		}
		messages = append(messages, out)
	}
	return messages, nil
}
