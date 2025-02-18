package types

// WellKnown is a struct for a well-known response.
type WellKnown struct {
	// Subject string `json:"subject"`
	Links []WellKnownLink `json:"links"`
}

// WellKnownLink is a struct for the links field of a well-known response.
type WellKnownLink struct {
	Rel  string `json:"rel"`
	Href string `json:"href"`
}

// WebFinger is a struct for a WebFinger response.
type WebFinger struct {
	Subject string          `json:"subject"`
	Links   []WebFingerLink `json:"links"`
}

// WebFingerLink is a struct for the links field of a WebFinger response.
type WebFingerLink struct {
	Rel  string `json:"rel"`
	Type string `json:"type"`
	Href string `json:"href"`
}

// ---------------------------------------------------------------------

// NodeInfo is a struct for a NodeInfo response.
type NodeInfo struct {
	Version           string           `json:"version,omitempty" yaml:"version"`
	Software          NodeInfoSoftware `json:"software,omitempty" yaml:"software"`
	Protocols         []string         `json:"protocols,omitempty" yaml:"protocols"`
	OpenRegistrations bool             `json:"openRegistrations,omitempty" yaml:"openRegistrations"`
	Metadata          NodeInfoMetadata `json:"metadata,omitempty" yaml:"metadata"`
}

// NodeInfoSoftware is a struct for the software field of a NodeInfo response.
type NodeInfoSoftware struct {
	Name    string `json:"name,omitempty" yaml:"name"`
	Version string `json:"version,omitempty" yaml:"version"`
}

// NodeInfoMetadata is a struct for the metadata field of a NodeInfo response.
type NodeInfoMetadata struct {
	NodeName        string                     `json:"nodeName,omitempty" yaml:"nodeName"`
	NodeDescription string                     `json:"nodeDescription,omitempty" yaml:"nodeDescription"`
	Maintainer      NodeInfoMetadataMaintainer `json:"maintainer,omitempty" yaml:"maintainer"`
	ThemeColor      string                     `json:"themeColor,omitempty" yaml:"themeColor"`
	ProxyCCID       string                     `json:"proxyCCID,omitempty" yaml:"proxyCCID"`
}

// NodeInfoMetadataMaintainer is a struct for the maintainer field of a NodeInfo response.
type NodeInfoMetadataMaintainer struct {
	Name  string `json:"name,omitempty" yaml:"name"`
	Email string `json:"email,omitempty" yaml:"email"`
}

// ---------------------------------------------------------------------

type ApObject struct {
	Context           any              `json:"@context,omitempty"`
	Actor             string           `json:"actor,omitempty"`
	Type              string           `json:"type,omitempty"`
	ID                string           `json:"id,omitempty"`
	To                any              `json:"to,omitempty"`
	CC                any              `json:"cc,omitempty"`
	Tag               any              `json:"tag,omitempty"`
	Attachment        []Attachment     `json:"attachment,omitempty"`
	InReplyTo         string           `json:"inReplyTo,omitempty"`
	Content           string           `json:"content,omitempty"`
	MisskeyContent    string           `json:"_misskey_content,omitempty"`
	Published         string           `json:"published,omitempty"`
	AttributedTo      string           `json:"attributedTo,omitempty"`
	QuoteURL          string           `json:"quoteUrl,omitempty"`
	Inbox             string           `json:"inbox,omitempty"`
	Outbox            string           `json:"outbox,omitempty"`
	SharedInbox       string           `json:"sharedInbox,omitempty"`
	Endpoints         *PersonEndpoints `json:"endpoints,omitempty"`
	Followers         string           `json:"followers,omitempty"`
	Following         string           `json:"following,omitempty"`
	Liked             string           `json:"liked,omitempty"`
	PreferredUsername string           `json:"preferredUsername,omitempty"`
	Name              string           `json:"name,omitempty"`
	Summary           string           `json:"summary,omitempty"`
	URL               string           `json:"url,omitempty"`
	Icon              Icon             `json:"icon,omitempty"`
	PublicKey         *Key             `json:"publicKey,omitempty"`
	Object            any              `json:"object,omitempty"`
	Sensitive         bool             `json:"sensitive,omitempty"`
	AlsoKnownAs       []string         `json:"alsoKnownAs,omitempty"`
}

type PersonEndpoints struct {
	SharedInbox string `json:"sharedInbox,omitempty"`
}

// Key is a struct for the publicKey field of an actor.
type Key struct {
	ID           string `json:"id,omitempty"`
	Type         string `json:"type,omitempty"`
	Owner        string `json:"owner,omitempty"`
	PublicKeyPem string `json:"publicKeyPem,omitempty"`
}

// Icon is a struct for the icon field of an actor.
type Icon struct {
	Type      string `json:"type,omitempty"`
	MediaType string `json:"mediaType,omitempty"`
	URL       string `json:"url,omitempty"`
}

// Attachment is a struct for an ActivityPub attachment.
type Attachment struct {
	Type      string `json:"type,omitempty"`
	MediaType string `json:"mediaType,omitempty"`
	URL       string `json:"url,omitempty"`
	Sensitive bool   `json:"sensitive,omitempty"`
}

// Tag is a struct for an ActivityPub tag.
type Tag struct {
	Type string `json:"type,omitempty"`
	ID   string `json:"id,omitempty"`
	Name string `json:"name,omitempty"`
	Icon Icon   `json:"icon,omitempty"`
	Href string `json:"href,omitempty"`
}

// ---------------------------------------------------------------------

type ApConfig struct {
	FQDN      string `yaml:"fqdn"`
	ProxyPriv string `yaml:"proxyPriv"`

	// internal generated
	ProxyCCID string
}

type AccountStats struct {
	Follows   []string `json:"follows"`
	Followers []string `json:"followers"`
}
