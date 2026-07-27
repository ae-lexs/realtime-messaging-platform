package firestore

import (
	"github.com/aelexs/realtime-messaging-platform/internal/domain"
)

// Collection names (ADR-023 "Firestore model").
const (
	CollectionUsers       = "users"
	CollectionChats       = "chats"
	CollectionMemberships = "memberships"
	CollectionSessions    = "sessions"

	// CollectionOTPRequests holds pending and recently-consumed OTPs
	// (ADR-023 v1.2, ADR-015 §1.1). Keyed by the SHA-256 phone hash.
	CollectionOTPRequests = "otp_requests"

	// CollectionPhoneIndex holds one document per claimed phone number — the
	// uniqueness sentinel, not a lookup index despite the name it inherits
	// from ADR-015 §5.1. Reads by phone go to users.phone_number.
	CollectionPhoneIndex = "phone_index"
)

const (
	// membershipIDSeparator joins the two IDs in a membership document ID.
	membershipIDSeparator = "__"

	// MaxDocIDBytes is Firestore's document-ID limit. The composite membership
	// ID is the only ID this project constructs rather than generates, so it is
	// the only one that could approach the bound; TestMembershipDocIDFitsBound
	// pins the margin.
	MaxDocIDBytes = 1500
)

// MembershipDocID builds the deterministic membership document ID,
// {chat_id}__{user_id} (ADR-023).
//
// Determinism is what makes "is this user in this chat?" a strongly consistent
// single get() instead of a query, while `where('chat_id','==',c)` and
// `where('user_id','==',u)` still serve both list directions off the automatic
// single-field indexes — one document, no manual index, all three patterns.
//
// It also means a delete followed by a re-set overwrites joined_at: the
// collection holds *current* membership, not history. Membership history lives
// in lake.raw_memberships_changed, fed by the memberships.changed events
// (ADR-022 D2).
func MembershipDocID(chatID domain.ChatID, userID domain.UserID) string {
	return chatID.String() + membershipIDSeparator + userID.String()
}
