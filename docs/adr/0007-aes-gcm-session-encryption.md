# 7. Encrypt MTProto Session Strings with AES-256-GCM

We decided to encrypt serialized MTProto session strings in the SQLite database using AES-256-GCM authenticated encryption. The encryption key is derived from the `TELEDRIVE_SECRET_KEY` environment variable (falling back to a restricted local keyfile). This prevents complete account takeover if database backups or snapshots are inadvertently exposed.
