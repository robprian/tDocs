# 6. Use Client-Side Chunked HTTP Upload Protocol

We decided to implement custom 5 MB–10 MB client-side chunked HTTP uploads rather than relying on a single multipart POST stream or an external protocol server (e.g. tusd). Slicing files in the browser allows robust retry of individual failed chunks over unstable network connections without forcing full-file retransmissions, while keeping server implementation lean and stdlib-compatible.
