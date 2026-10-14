# Palette attribution

The semantic palettes in `themes.go` adapt the following upstream color schemes.
Some status and muted colors are adjusted for contrast in the terminal browser.
Only palette values are adapted; no editor plugin code is included.

| Palette | Source | License copy |
| --- | --- | --- |
| Nord | https://github.com/nordtheme/nord | [MIT](licenses/nord.txt) |
| Rosé Pine | https://github.com/rose-pine/rose-pine-theme | [MIT](licenses/rose-pine.txt) |
| Catppuccin | https://github.com/catppuccin/catppuccin | [MIT](licenses/catppuccin.txt) |
| Dracula | https://github.com/dracula/dracula-theme | [MIT](licenses/dracula.txt) |
| Gruvbox | https://github.com/morhetz/gruvbox | [MIT/X11](licenses/gruvbox.txt) |
| Tokyo Night | https://github.com/folke/tokyonight.nvim | [Apache-2.0](licenses/tokyo-night.txt) |
| One Dark | https://github.com/atom/one-dark-syntax | [MIT](licenses/one-dark.txt) |
| Solarized | https://github.com/altercation/solarized | [MIT](licenses/solarized.txt) |
| Ayu (Dark, Mirage, Light) | [ayu-colors](https://github.com/ayu-theme/ayu-colors/tree/b0fd979a1ddf050101b43311fa598a1a9c5f1bbc) | [MIT](licenses/ayu.txt) |
| Everforest (Dark/Light, Hard/Medium/Soft) | [Everforest palette](https://github.com/sainnhe/everforest/blob/85a86eb62409e3ec88713bff3d1b9d7374e112e4/palette.md) | [MIT](licenses/everforest.txt) |

Ayu's translucent selection backgrounds are composited into opaque terminal
colours. Muted/foreground and status/accent colours are adjusted where needed
for readable contrast. Everforest retains all six background variants;
selection uses the corresponding popup background.

Retain these notices when distributing the bundled palettes.
