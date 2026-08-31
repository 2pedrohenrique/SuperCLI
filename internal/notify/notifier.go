package notify

import "github.com/gen2brain/beeep"

type Notifier interface {
	Notify(title, message string) error
}

type Desktop struct{}

func NewDesktop() Desktop {
	beeep.AppName = "SuperCLI"
	return Desktop{}
}

func (Desktop) Notify(title, message string) error {
	return beeep.Notify(title, message, "")
}

type Noop struct{}

func (Noop) Notify(string, string) error { return nil }
