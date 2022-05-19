package bigtext

import (
	"github.com/andybrewer/mack"
)

const Version = "0.3"

// A Client is a vehicle for displaying information. Some properties may not
// be available in different notifiers
type Client struct {
	// Name is the name of the user or thing sending the command.
	Name string
}

var DefaultClient = Client{
	Name: "Terminal",
}

func (c *Client) Display(text string) error {
	return mack.Notify(text, c.Name)
}

// Display text in large type. We try to use the terminal-notifier app if it's
// present, and try Quicksilver.app if terminal-notifier is not present. If
// neither application is present, an error is returned.
func Display(text string) error {
	return DefaultClient.Display(text)
}
