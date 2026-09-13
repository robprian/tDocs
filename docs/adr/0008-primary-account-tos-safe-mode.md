# 8. Primary Account Safe Mode and Strict Telegram ToS Compliance

We decided to enforce a strict "Safe Mode" traffic profile to allow users to safely run TeleDrive using their primary personal Telegram account without risk of account termination. Telegram anti-abuse heuristics flag aggressive multi-threading, spoofed client metadata, and high-volume public file proxying. We enforce personal API credentials from my.telegram.org, sequential single-worker uploads, organic client telemetry headers, strictly private storage channels, and defensive FLOOD_WAIT cooldowns.
