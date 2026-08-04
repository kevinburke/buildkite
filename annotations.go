package main

import (
	"os"

	"charm.land/glamour/v2"
	"github.com/JohannesKaufmann/html-to-markdown/v2/converter"
	"github.com/JohannesKaufmann/html-to-markdown/v2/plugin/base"
	"github.com/JohannesKaufmann/html-to-markdown/v2/plugin/commonmark"
	"github.com/JohannesKaufmann/html-to-markdown/v2/plugin/table"
	buildkite "github.com/kevinburke/buildkite/lib"
	"github.com/muesli/termenv"
	"golang.org/x/term"
)

func getTerminalWidth() int {
	width, _, err := term.GetSize(int(os.Stdout.Fd()))
	if err != nil || width <= 0 {
		return 80 // fallback width if we can't determine the actual width
	}
	return width
}

func autoStylePath() string {
	if termenv.HasDarkBackground() {
		return "dark"
	}
	return "light"
}

func getANSIAnnotations(annotations buildkite.AnnotationResponse) ([]string, error) {
	width := min(getTerminalWidth(), 120)
	renderer, err := glamour.NewTermRenderer(
		glamour.WithStylePath(autoStylePath()),
		glamour.WithWordWrap(width),
	)
	if err != nil {
		return nil, err
	}
	var messages []string
	conv := converter.NewConverter(
		converter.WithPlugins(
			base.NewBasePlugin(),
			commonmark.NewCommonmarkPlugin(),
			table.NewTablePlugin(),
		),
	)
	for _, annotation := range annotations {
		content, err := conv.ConvertString(annotation.BodyHTML)
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
