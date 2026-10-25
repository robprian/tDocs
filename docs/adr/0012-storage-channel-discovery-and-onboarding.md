# 12. Storage Channel Auto-Discovery and Interactive Onboarding

We decided to implement automated Storage Channel Discovery and interactive onboarding during CLI authentication (`tdocs login`) and disaster recovery (`tdocs restore`).

Previously, tDocs only inspected local SQLite settings to determine the active Storage Channel. When a user authenticated from a new workstation or secondary server without migrating their local `tdocs.db`, tDocs mistakenly assumed the user had no Storage Channel and created a duplicate private channel titled "tDocs Vault" in Telegram. This left the new machine with an empty virtual file system and caused channel proliferation on Telegram.

Under the new design, tDocs queries MTProto dialogs (`messages.getDialogs`) during onboarding to discover existing "tDocs Vault" broadcast channels. If a single channel is found, tDocs offers to link to it and optionally restore the latest Database Snapshot immediately. If multiple channels exist, tDocs presents a numbered disambiguation list showing channel IDs, creation dates, and snapshot counts. If no existing channel is discovered, a new Storage Channel is created automatically. In non-interactive or containerized environments, the channel can be explicitly bound via `TDOCS_STORAGE_CHANNEL_ID`.
