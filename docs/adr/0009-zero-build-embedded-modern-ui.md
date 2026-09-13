# 9. Zero-Build Embedded Modern UI System

We decided to build the web interface using standard HTML5, CSS custom properties, modern Vanilla JavaScript, and embedded SVG iconography, served directly via Go's `embed.FS` without an external Node.js or npm build pipeline (such as Vite, Webpack, or Tailwind CLI).

This preserves TeleDrive's single-binary portability and zero-dependency compilation (`go build`), eliminates supply chain risks from npm packages, and avoids build-step divergence. Modern CSS variables provide full light/dark theme switching, while inline vector iconography delivers sharp, resolution-independent visuals with zero additional network round-trips.
