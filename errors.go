package obsidian

import "errors"

var (
	// ErrEmptyRoot is returned when a vault is opened with an empty root path.
	ErrEmptyRoot = errors.New("obsidian: empty vault root")
	// ErrInvalidKey is returned for a key that cannot name a vault note.
	ErrInvalidKey = errors.New("obsidian: invalid key")
	// ErrInvalidKind is returned for a node whose Kind cannot be written to a note's frontmatter.
	ErrInvalidKind = errors.New("obsidian: invalid kind")
	// ErrInvalidLink is returned for a link whose relation or target a wikilink cannot carry.
	ErrInvalidLink = errors.New("obsidian: invalid link")
	// ErrNoProject is returned by Init when the spec names no project.
	ErrNoProject = errors.New("obsidian: Init needs a Project name")
)
