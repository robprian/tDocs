# 10. Point-in-Time Database Snapshots and Rolling Retention Policy

We decided to implement point-in-time database snapshot management with an automated rolling retention policy directly through the Telegram Storage Channel and Web UI.

Rather than storing snapshots indefinitely or relying on external cloud backup services, TeleDrive stores gzip-compressed SQLite snapshots (`VACUUM INTO`) as documents in the private Storage Channel. To prevent storage clutter and stay within rate limits, a rolling retention policy retains the 5 newest snapshots and automatically prunes older snapshot messages using MTProto's `messages.deleteMessages`.

Online restoration from the Web UI uses connection draining and hot-swapping under an exclusive read-write lock (`dbMu`), closing active SQLite connections, replacing the database file atomically, and reopening the database without requiring process termination.
