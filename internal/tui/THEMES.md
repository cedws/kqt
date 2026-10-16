# Palette attribution

The semantic palettes in `themes.go` adapt the following upstream color schemes.
Some status and muted colors are adjusted for contrast in the terminal browser.
Only palette values are adapted; no editor plugin code is included.

The built-in `no-color` theme uses terminal-default colours throughout,
including text inputs. It keeps bold/underlined text and uses reverse video
for selections without setting a foreground or background colour.

| Palette | Source | License copy |
| --- | --- | --- |
| Nord | https://github.com/nordtheme/nord | [MIT](licenses/nord.txt) |
| Rosé Pine | https://github.com/rose-pine/rose-pine-theme | [MIT](licenses/rose-pine.txt) |
| Catppuccin | https://github.com/catppuccin/catppuccin | [MIT](licenses/catppuccin.txt) |
| Dracula | https://github.com/dracula/dracula-theme | [MIT](licenses/dracula.txt) |
| Material (Default) | [material.vim](https://github.com/kaicataldo/material.vim/blob/32f423c825ee89a37d66e3d8f00e777c7d8a41ab/colors/material.vim) | [MIT](licenses/material.txt) |
| Palenight | [Palenight Theme for VS Code](https://github.com/whizkydee/vscode-palenight-theme/blob/63949af027921fd09466c1e6003bd2b994e14a9e/themes/palenight.json) | [MIT](licenses/palenight.txt) |
| Darcula | [Darcula for VS Code](https://github.com/rokoroku/vscode-theme-darcula/blob/657baba5dc1d632daefe8e6130abcca550f0ea1f/themes/darcula.json) | [MIT](licenses/darcula.txt) |
| Monokai | [VS Code's built-in Monokai](https://github.com/microsoft/vscode/blob/f20a64b96be4f2b10d7cd2703efb2fc85afaac18/extensions/theme-monokai/themes/monokai-color-theme.json) | [MIT](licenses/monokai.txt) |
| Ubuntu | [Radu Matei's Ubuntu Color Theme for VS Code](https://github.com/radu-matei/vscode-ubuntu-color-theme/blob/dbe2e82cfdda6ffb1e0cbe97b4cda96fcf18dda9/themes/ubuntu-color-theme.json) | [MIT](licenses/ubuntu.txt) |
| Lemonade | [Denner's Lemonade for VS Code](https://github.com/dennerrondinely/lemonade/blob/320c9e6ae95bf6478804ca85a75897b11156a536/themes/lemonade-color-theme.json) | [MIT](licenses/lemonade.txt) |
| Pink Lemonade (Light) | [Casseopeian's VS Code theme](https://github.com/gbmyt/pinkvscodetheme/tree/e620f7ac095092c8236181630a00d6e064ceb5f2) | [Attribution and upstream license status](licenses/pink-lemonade.txt) |
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

Material uses its original blue-grey background and cyan accent; Palenight
uses the purple accent and composited editor selection from its VS Code theme.
Darcula and Monokai retain their editor and panel backgrounds with brighter
muted/status text where needed. Ubuntu uses Radu Matei's aubergine backgrounds
and orange accent, with an opaque composite of the editor selection. Lemonade
uses Denner's charcoal backgrounds, turquoise accent, mint strings, and yellow
numbers. Pink Lemonade preserves its light pink editor,
sidebar, and selection backgrounds; text and status colours are darkened for
terminal readability. Its upstream repository supplies no explicit license;
only its palette values are referenced here, with attribution retained.

Retain these notices when distributing the bundled palettes.

## Accessible presets

`accessible-dark` and `accessible-light` are kqt palettes informed by
[Okabe and Ito's Color Universal Design guidance](https://jfly.uni-koeln.de/color/).
They use blue for success/information, yellow or dark amber for attention,
and reddish purple for problems. The light preset darkens these accents for
text on pale backgrounds. These are design choices, not a claim that one set
of colours covers every person's colour vision.

Both presets target at least 4.5:1 for normal, muted, status, and input text on
the main and panel backgrounds, and for selected text on its selection fill.
Borders/focus cues and selection fills target 3:1 against adjacent backgrounds.
Those targets use [WCAG text contrast](https://www.w3.org/WAI/WCAG22/Understanding/contrast-minimum.html)
and [non-text contrast](https://www.w3.org/WAI/WCAG22/Understanding/non-text-contrast.html)
as design guidance for this terminal interface, without asserting WCAG conformance.
Checks include the standard xterm 256-colour conversion. Custom terminal
palettes, display settings, and fonts can affect the result.

Meaning is also available without colour: table/picker selection markers,
focused-pane heading markers, written status/log levels, underlined log
matches, `+`/`-` diffs, and `OK`/`Attention`/`Problem` labels for known condition
predicates. Unknown custom conditions keep their reported values without an
invented health interpretation. Inputs follow semantic theme colours,
including inherited custom themes.

These matching demo previews contain sample data only:

| Accessible dark | Accessible light | No colour (example terminal defaults) |
| --- | --- | --- |
| [Preview](../../docs/screenshots/accessible-dark.png) | [Preview](../../docs/screenshots/accessible-light.png) | [Preview](../../docs/screenshots/no-color.png) |

The comparison was screened in greyscale and with full-severity protan/deutan
simulations using Machado (2009), plus Brettel (1997) for tritan simulation,
through [DaltonLens](https://github.com/DaltonLens/DaltonLens-Python/tree/3cba5e6a7c8f0e8199c8f83f1afb58eb6dab7a3d).
Simulation is approximate. Feedback from users with colour-vision deficiencies
is still needed; the presets have not been qualified by a user study.
The `no-color` preview uses example dark terminal defaults; its actual contrast
is controlled by the user's terminal.
