// Copyright (c) 2024 The Metanet developers
// Use of this source code is governed by the Open BSV License v5
// that can be found in the LICENSE file.

package overlay

// Predefined overlay topics.
const (
	TopicStorage = "metanet.storage" // Storage deal announcements.
	TopicProof   = "metanet.proof"   // Storage proof submissions.
	TopicPayment = "metanet.payment" // Payment channel state.
	TopicContent = "metanet.content" // Content availability announcements.
)

// AllTopics returns all predefined overlay topics.
func AllTopics() []string {
	return []string{TopicStorage, TopicProof, TopicPayment, TopicContent}
}
