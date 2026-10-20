# siq_converter
Simple SIQ (SI game questions) to Yaml and back converter

`siqconv` turns a SIGame question package (`.siq`, format version 5) into a folder with a human-editable
`content.yaml` plus media files, and packs such a folder back into a `.siq`.

```sh
go build -o siqconv ./cmd/siqconv

siqconv init pkg                  # new package: commented example of every question type
siqconv unpack pkg.siq            # -> pkg/content.yaml, pkg/Images/..., pkg/Audio/...
siqconv pack pkg                  # -> pkg.siq
siqconv convert <path>            # picks the direction itself
siqconv verify pkg.siq            # checks that .siq -> YAML -> .siq loses nothing
```

`-f` overwrites an existing output.

### Desktop app

`siqconv-ui` is a small window for people who don't use the command line. It has three tabs:
- **SIQ to folder:** unpack a `.siq`, or verify it.
- **Folder to SIQ:** pack a package folder.
- **New package:** create a package from the example template.

You can also drop a `.siq` file, a package folder or its `content.yaml` onto the window.

```sh
go build -o siqconv-ui ./cmd/siqconv-ui                                   # macOS / Linux
go build -ldflags "-H=windowsgui" -o siqconv-ui.exe ./cmd/siqconv-ui      # Windows, no console window
```

It is built with [Fyne](https://fyne.io), which needs cgo:
- **macOS:** Xcode command line tools (`xcode-select --install`).
- **Windows:** a GCC such as MSYS2 or TDM-GCC.
- **Linux:** the X11/OpenGL dev packages (see Fyne's docs).

Building for Windows from a Mac requires [fyne-cross](https://github.com/fyne-io/fyne-cross), which runs in Docker.

[`example/example_pack`](example) is a complete example package with public-domain media. Pack it and open it in SIGame.

`init` creates `pkg/` with empty media folders and a commented `content.yaml`. It covers:
- every question type: simple, stake, secret, secretPublicPrice, secretNoQuestion, noRisk, forAll, and stakeAll in the final round
- every content item kind
- the select, number and point answer types

The media files it references are **not** created. The comment at the top lists them, and `pack` reports any still missing. YAML comments exist only in this template; `pack` + `unpack` does not keep them.

## Folder layout

```
pkg/
  content.yaml
  Images/   image items and the logo
  Audio/    audio items
  Video/    video items
  Html/     html items
  ...       any other files (e.g. quality.marker) are kept and packed as is
```

Files use their real (decoded) names. Hidden files (`.DS_Store`, …) are ignored.

`pack` stops if a file the YAML references is missing, and warns about media files nothing references.

## What is regenerated on pack

These are derived data, so they are never stored in YAML:
- **`<files>`:** the hash list, an uppercase hex SHA-256 of every media file, computed exactly as SIGame does.
- **Package `id`:** if `content.yaml` has no `id`, a new UUID is generated on every pack. Packages unpacked from a `.siq` keep theirs.
- **XML formatting:** SIGame's element and attribute order, `True`/`False`, the BOM, and the namespace.
- **Zip layout:** `content.xml` comes first, and media is stored uncompressed. Entry names are percent-encoded as SIGame does it.

Everything else round-trips exactly: structure, text including whitespace, attributes, item settings, info, and global authors and sources.

## content.yaml

```yaml
siq: 5                       # format version (optional, default 5)
name: My package             # required
id: 80d2a5b9-...             # optional
date: 17.10.2026
publisher: me
contactUri: https://...
difficulty: 5
logo: "@logo.png"            # "@" = file in Images/
language: ru-RU
restriction: 18+
tags: [Music, Movies]
info:                        # allowed on package, round, theme and question
  authors: [Me, "@a1"]       # "@id" refers to global.authors
  sources: ["@s1#p. 12"]
  comments: |
    Multi-line text
  showmanComments: Read this aloud
global:
  authors: [{id: a1, name: Ivan, surname: Petrov, country: RU, city: Moscow}]
  sources: [{id: s1, author: Pushkin, title: Onegin, year: 1833}]

rounds:
  - name: Round 1
    themes:
      - name: Movies
        questions:
          - price: 100
            params:
              question:
                - Which movie is this?        # text item
                - image: frame.jpg            # file from Images/
              answer:                         # optional
                - image: poster.jpg
            right: Interstellar               # one answer

  - name: Final
    type: final
    themes:
      - name: Science
        questions:
          - price: 0
            params:
              question: [Name the formula]
            right: [RbBr, rubidium bromide]   # several accepted answers
            wrong: RbCl                       # optional
```

### Answers

`right` and `wrong` take a single value or a list. Notice the difference:

```yaml
right: Guardians of the Galaxy, American Psycho      # ONE answer containing a comma
right: [Guardians of the Galaxy, American Psycho]    # TWO accepted answers
```

`right: []` means no answer.

### Content items

| YAML | Meaning |
|---|---|
| `- Some text` | text on screen |
| `- text: Hello`<br>`  placement: replic` | text spoken by the showman |
| `- image: a.jpg` | image from `Images/` |
| `- audio: a.mp3` | audio from `Audio/` (plays in the background by default) |
| `- video: a.mp4` | video from `Video/` |
| `- html: a.html` | html from `Html/` |
| `- image: https://...`<br>`  ref: false` | external file instead of a package file |

Extra item keys:
- **`placement`:** `screen`, `replic` or `background`. The default is `background` for audio and `screen` for everything else.
- **`wait: false`:** don't wait for this item to finish, so it shows together with the next one.
- **`duration`:** in the form `"00:00:05"`.

### Question types and parameters

`type` is a well-known question type (`stake`, `stakeAll`, `secret`, `secretPublicPrice`, `secretNoQuestion`, `noRisk`) or a custom one. If it's omitted, the question is `simple`.

The parameter type is inferred from the shape of its value:

```yaml
            type: secret
            params:
              question: [Some text]                                     # list   -> content
              theme: Custom theme                                       # scalar -> simple
              selectionMode: exceptCurrent
              price:
                numberSet: {minimum: 100, maximum: 500, step: 100}      # numberSet
              options:
                group: {a: 1, b: 2}                                     # group of parameters
```

Rarely used parameter forms (a parameter with `isRef`, or an unknown parameter type) are written as `{raw: {type, ref, value}}`.

Questions may also have a `script:`, which is a list of steps, each a parameters map like `params`.

### Strictness

Unknown keys in YAML and unknown elements or attributes in `content.xml` are errors that name the line or path, so a typo never loses data silently. SIQ format version 4 packages are not supported.

## Development

```sh
go test ./...
go test ./internal/siqyaml -update    # regenerate testdata/full.yaml after intentional format changes
```
