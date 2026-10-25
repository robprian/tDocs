# 1. Use Dedicated Private Channel for Storage

We decided to store all binary files and metadata snapshots in a dedicated private Telegram channel rather than the user's Saved Messages. Saved Messages pollutes personal chat history, cannot be partitioned, and lacks an isolated message ID namespace. A dedicated private channel provides a clean, isolated object store with independent message IDs and supports pinned recovery snapshots.
