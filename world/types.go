package world

type Profile struct {
	Username    string    `json:"username"`
	Avatar      string    `json:"avatar"`
	Description string    `json:"description"`
	Banner      string    `json:"banner"`
	Subprofiles *[]string `json:"subprofiles"`
}

type Emoji struct {
	ImageURL string `json:"imageURL"`
}

type ProfileOverride struct {
	Username    string `json:"username,omitempty"`
	Avatar      string `json:"avatar,omitempty"`
	Description string `json:"description,omitempty"`
	Link        string `json:"link,omitempty"`
	CharacterID string `json:"characterID,omitempty"`
}

type MarkdownMessage struct {
	Body            string            `json:"body"`
	Emojis          *map[string]Emoji `json:"emojis,omitempty"`
	ProfileOverride *ProfileOverride  `json:"profileOverride,omitempty"`
}

type Media struct {
	MediaURL     string `json:"mediaURL"`
	MediaType    string `json:"mediaType"`
	ThumbnailURL string `json:"thumbnailURL,omitempty"`
	Blurhash     string `json:"blurhash,omitempty"`
}

type MediaMessage struct {
	Body            string            `json:"body"`
	Emojis          *map[string]Emoji `json:"emojis,omitempty"`
	Medias          *[]Media          `json:"medias,omitempty"`
	ProfileOverride *ProfileOverride  `json:"profileOverride,omitempty"`
}

type ReactionAssociation struct {
	ImageURL        string           `json:"imageUrl"`
	Shortcode       string           `json:"shortcode"`
	ProfileOverride *ProfileOverride `json:"profileOverride"`
}

type LikeAssociation struct {
	ProfileOverride *ProfileOverride `json:"profileOverride"`
}

type ReplyAssociation struct {
	MessageID       string           `json:"messageId"`
	MessageAuthor   string           `json:"messageAuthor"`
	ProfileOverride *ProfileOverride `json:"profileOverride"`
}

type ReplyMessage struct {
	ReplyToMessageID     string            `json:"replyToMessageId"`
	ReplyToMessageAuthor string            `json:"replyToMessageAuthor"`
	Body                 string            `json:"body"`
	Emojis               *map[string]Emoji `json:"emojis"`
	ProfileOverride      *ProfileOverride  `json:"profileOverride"`
}

type RerouteAssociation struct {
	MessageID       string           `json:"messageId"`
	MessageAuthor   string           `json:"messageAuthor"`
	ProfileOverride *ProfileOverride `json:"profileOverride"`
}

type RerouteMessage struct {
	RerouteMessageID     string            `json:"rerouteMessageId"`
	RerouteMessageAuthor string            `json:"rerouteMessageAuthor"`
	Body                 string            `json:"body"`
	Emojis               *map[string]Emoji `json:"emojis"`
	ProfileOverride      *ProfileOverride  `json:"profileOverride"`
}
