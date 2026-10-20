// Package model is the in-memory representation of a SIQ (version 5) package,
// shared by the XML and YAML converters.
//
// Empty values mean "absent / default": an empty slice is nil, an empty string
// is omitted, and so on. Both converters normalize to this form so that two
// semantically equal packages compare equal with reflect.DeepEqual.
package model

// Version is the SIQ format version this model describes.
const Version = "5"

// Content item types.
const (
	ItemText  = "text"
	ItemImage = "image"
	ItemAudio = "audio"
	ItemVideo = "video"
	ItemHTML  = "html"
)

// Content item placements.
const (
	PlacementScreen     = "screen"
	PlacementReplic     = "replic"
	PlacementBackground = "background"
)

// Parameter types.
const (
	ParamSimple    = "simple"
	ParamContent   = "content"
	ParamGroup     = "group"
	ParamNumberSet = "numberSet"
)

// Media collection (folder) names inside the archive.
const (
	FolderImages = "Images"
	FolderAudio  = "Audio"
	FolderVideo  = "Video"
	FolderHTML   = "Html"
)

// MediaFolders lists the media collections in their canonical archive order.
var MediaFolders = []string{FolderImages, FolderAudio, FolderVideo, FolderHTML}

type Package struct {
	Version     string
	Name        string
	ID          string
	Restriction string
	Date        string
	Publisher   string
	ContactURI  string
	Difficulty  int // 0 means absent
	Logo        string
	Language    string
	Generator   string
	Tags        []string
	Info        Info
	Global      Global
	Rounds      []Round
}

type Info struct {
	Authors         []string
	Sources         []string
	Comments        string
	ShowmanComments string
	Extension       string
}

func (i Info) IsEmpty() bool {
	return len(i.Authors) == 0 && len(i.Sources) == 0 && i.Comments == "" && i.ShowmanComments == "" && i.Extension == ""
}

type Global struct {
	Authors []AuthorInfo
	Sources []SourceInfo
}

func (g Global) IsEmpty() bool { return len(g.Authors) == 0 && len(g.Sources) == 0 }

type AuthorInfo struct {
	ID         string
	Name       string
	SecondName string
	Surname    string
	Country    string
	City       string
}

type SourceInfo struct {
	ID      string
	Author  string
	Title   string
	Year    int // 0 means absent; SIGame stores it as an integer
	Publish string
	City    string
}

type Round struct {
	Name   string
	Type   string
	Info   Info
	Themes []Theme
}

type Theme struct {
	Name      string
	Info      Info
	Questions []Question
}

type Question struct {
	Price  int
	Type   string
	Info   Info
	Params []Param
	Script [][]Param // each element is one script step
	Right  []string
	Wrong  []string
}

type Param struct {
	Name      string
	Type      string // "" means simple
	IsRef     bool
	Value     string     // simple value (also used when IsRef is set)
	Items     []Item     // content value
	Group     []Param    // group value
	NumberSet *NumberSet // numberSet value
}

type NumberSet struct {
	Minimum *int
	Maximum *int
	Step    *int
}

type Item struct {
	Type      string // "" means text
	Value     string
	IsRef     bool
	Placement string // "" means the default placement for the type
	Duration  string
	NoWait    bool // waitForFinish="False"
}

// NormalizeRight maps a single empty right answer to no answers: SIGame adds
// one empty answer to a question without any, so the two are equivalent.
func NormalizeRight(right []string) []string {
	if len(right) == 1 && right[0] == "" {
		return nil
	}
	return right
}

// DefaultPlacement returns the placement SIGame assumes when none is given.
func DefaultPlacement(itemType string) string {
	if itemType == ItemAudio {
		return PlacementBackground
	}
	return PlacementScreen
}

// MediaFolder returns the archive folder holding referenced files of the given item type.
func MediaFolder(itemType string) (string, bool) {
	switch itemType {
	case ItemImage:
		return FolderImages, true
	case ItemAudio, "voice":
		return FolderAudio, true
	case ItemVideo:
		return FolderVideo, true
	case ItemHTML:
		return FolderHTML, true
	}
	return "", false
}

// WalkItems calls fn for every content item in the package.
func (p *Package) WalkItems(fn func(it Item)) {
	var walk func(ps []Param)
	walk = func(ps []Param) {
		for _, prm := range ps {
			for _, it := range prm.Items {
				fn(it)
			}
			walk(prm.Group)
		}
	}
	for _, r := range p.Rounds {
		for _, t := range r.Themes {
			for _, q := range t.Questions {
				walk(q.Params)
				for _, step := range q.Script {
					walk(step)
				}
			}
		}
	}
}
