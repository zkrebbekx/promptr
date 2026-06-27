package promptr

// PartKind tags a multimodal message Part.
type PartKind uint8

const (
	// PartText is a plain-text span.
	PartText PartKind = iota
	// PartImage is an image (PNG/JPEG/…), by inline bytes or URL.
	PartImage
	// PartAudio is an audio clip.
	PartAudio
	// PartFile is a document (e.g. a PDF), by inline bytes or URL.
	PartFile
)

// Part is one piece of a multimodal message: text, an image, audio, or a file.
// A media Part carries either inline Data (+MIME) or a URL reference. Providers
// map Parts to their own content-array formats; a provider that does not support
// a Part kind may ignore or reject it.
type Part struct {
	Kind PartKind
	Text string // PartText
	MIME string // media: e.g. "image/png", "application/pdf"
	Data []byte // inline media bytes (base64-encoded by the provider)
	URL  string // or a reference to remote media (used when Data is empty)
}

// TextPart builds a text Part.
func TextPart(s string) Part { return Part{Kind: PartText, Text: s} }

// ImagePart builds an inline image Part from raw bytes and a MIME type.
func ImagePart(mime string, data []byte) Part {
	return Part{Kind: PartImage, MIME: mime, Data: data}
}

// ImageURL builds an image Part that references a remote URL.
func ImageURL(url string) Part { return Part{Kind: PartImage, URL: url} }

// FilePart builds an inline document Part (e.g. a PDF) from bytes and a MIME.
func FilePart(mime string, data []byte) Part {
	return Part{Kind: PartFile, MIME: mime, Data: data}
}

// AudioPart builds an inline audio Part from bytes and a MIME type.
func AudioPart(mime string, data []byte) Part {
	return Part{Kind: PartAudio, MIME: mime, Data: data}
}
