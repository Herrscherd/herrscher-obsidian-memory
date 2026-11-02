package obsidian

import "errors"

var (
	ErrEmptyRoot   = errors.New("obsidian: empty vault root")
	ErrInvalidKey  = errors.New("obsidian: invalid key")
	ErrInvalidLink = errors.New("obsidian: invalid link")
	ErrNoProject   = errors.New("obsidian: Init needs a Project name")
)
