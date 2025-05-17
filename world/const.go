package world

const (
	MarkdownMessageSchema = "https://schema.concrnt.world/m/markdown.json"
	MisskeyMessageSchema  = "https://schema.concrnt.world/m/mfm.json"
	MediaMessageSchema    = "https://schema.concrnt.world/m/media.json"
	ReplyMessageSchema    = "https://schema.concrnt.world/m/reply.json"
	RerouteMessageSchema  = "https://schema.concrnt.world/m/reroute.json"

	LikeAssociationSchema     = "https://schema.concrnt.world/a/like.json"
	MentionAssociationSchema  = "https://schema.concrnt.world/a/mention.json"
	ReplyAssociationSchema    = "https://schema.concrnt.world/a/reply.json"
	RerouteAssociationSchema  = "https://schema.concrnt.world/a/reroute.json"
	ReactionAssociationSchema = "https://schema.concrnt.world/a/reaction.json"

	ProfileSchema = "https://schema.concrnt.world/p/main.json"
)

const (
	UserHomeStream   = "world.concrnt.t-home"
	UserNotifyStream = "world.concrnt.t-notify"
	UserAssocStream  = "world.concrnt.t-assoc"
	UserApStream     = "world.concrnt.t-ap"
)
