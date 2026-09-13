# 3. Stream File Data Directly Without Disk Spooling

We decided to stream upload and download data directly between client HTTP connections and MTProto parts using memory-bounded chunk pipes, without spooling full files to local server disk. Spooling 2GB–4GB files would exhaust disk space on low-cost VPS environments and add redundant disk I/O.
