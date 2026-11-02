package obsidian

import "errors"

var (
	// ErrEmptyRoot is returned when a vault is opened with an empty root path.
	ErrEmptyRoot = errors.New("obsidian: empty vault root")
	// ErrInvalidKey is returned for a key that cannot name a vault note.
	ErrInvalidKey = errors.New("obsidian: invalid key")
	// ErrInvalidLink is returned for a link whose relation or target a wikilink cannot carry.
	ErrInvalidLink = errors.New("obsidian: invalid link")
	// ErrNoProject is returned by Init when the spec names no project.
	ErrNoProject = errors.New("obsidian: Init needs a Project name")
)
