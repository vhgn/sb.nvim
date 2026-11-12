package tex

// Atom classes, used for TeX-style inter-atom spacing.
const (
	clsOrd = iota
	clsOp
	clsBin
	clsRel
	clsOpen
	clsClose
	clsPunct
	clsInner
)

type sym struct {
	s   string
	cls int
}

var symbols = map[string]sym{
	// greek
	"alpha": {"α", clsOrd}, "beta": {"β", clsOrd}, "gamma": {"γ", clsOrd}, "delta": {"δ", clsOrd},
	"epsilon": {"ϵ", clsOrd}, "varepsilon": {"ε", clsOrd}, "zeta": {"ζ", clsOrd}, "eta": {"η", clsOrd},
	"theta": {"θ", clsOrd}, "vartheta": {"ϑ", clsOrd}, "iota": {"ι", clsOrd}, "kappa": {"κ", clsOrd},
	"lambda": {"λ", clsOrd}, "mu": {"μ", clsOrd}, "nu": {"ν", clsOrd}, "xi": {"ξ", clsOrd},
	"omicron": {"ο", clsOrd}, "pi": {"π", clsOrd}, "varpi": {"ϖ", clsOrd}, "rho": {"ρ", clsOrd},
	"varrho": {"ϱ", clsOrd}, "sigma": {"σ", clsOrd}, "varsigma": {"ς", clsOrd}, "tau": {"τ", clsOrd},
	"upsilon": {"υ", clsOrd}, "phi": {"ϕ", clsOrd}, "varphi": {"φ", clsOrd}, "chi": {"χ", clsOrd},
	"psi": {"ψ", clsOrd}, "omega": {"ω", clsOrd},
	"Gamma": {"Γ", clsOrd}, "Delta": {"Δ", clsOrd}, "Theta": {"Θ", clsOrd}, "Lambda": {"Λ", clsOrd},
	"Xi": {"Ξ", clsOrd}, "Pi": {"Π", clsOrd}, "Sigma": {"Σ", clsOrd}, "Upsilon": {"Υ", clsOrd},
	"Phi": {"Φ", clsOrd}, "Psi": {"Ψ", clsOrd}, "Omega": {"Ω", clsOrd},
	// ordinary symbols
	"infty": {"∞", clsOrd}, "partial": {"∂", clsOrd}, "nabla": {"∇", clsOrd}, "hbar": {"ℏ", clsOrd},
	"ell": {"ℓ", clsOrd}, "emptyset": {"∅", clsOrd}, "varnothing": {"∅", clsOrd}, "aleph": {"ℵ", clsOrd},
	"forall": {"∀", clsOrd}, "exists": {"∃", clsOrd}, "nexists": {"∄", clsOrd}, "neg": {"¬", clsOrd}, "lnot": {"¬", clsOrd},
	"angle": {"∠", clsOrd}, "triangle": {"△", clsOrd}, "prime": {"′", clsOrd}, "degree": {"°", clsOrd},
	"ldots": {"…", clsInner}, "dots": {"…", clsInner}, "cdots": {"⋯", clsInner}, "vdots": {"⋮", clsOrd}, "ddots": {"⋱", clsInner},
	"Re": {"ℜ", clsOrd}, "Im": {"ℑ", clsOrd}, "wp": {"℘", clsOrd}, "top": {"⊤", clsOrd}, "bot": {"⊥", clsOrd},
	"checkmark": {"✓", clsOrd}, "star": {"⋆", clsBin}, "dagger": {"†", clsBin}, "S": {"§", clsOrd},
	"backslash": {"∖", clsOrd}, "lbrace": {"{", clsOpen}, "rbrace": {"}", clsClose}, "{": {"{", clsOpen}, "}": {"}", clsClose},
	"langle": {"⟨", clsOpen}, "rangle": {"⟩", clsClose}, "lfloor": {"⌊", clsOpen}, "rfloor": {"⌋", clsClose},
	"lceil": {"⌈", clsOpen}, "rceil": {"⌉", clsClose}, "vert": {"|", clsOrd}, "Vert": {"‖", clsOrd}, "|": {"‖", clsOrd},
	"#": {"#", clsOrd}, "$": {"$", clsOrd}, "%": {"%", clsOrd}, "&": {"&", clsOrd}, "_": {"_", clsOrd},
	// binary operators
	"pm": {"±", clsBin}, "mp": {"∓", clsBin}, "times": {"×", clsBin}, "div": {"÷", clsBin}, "cdot": {"⋅", clsBin},
	"ast": {"∗", clsBin}, "circ": {"∘", clsBin}, "bullet": {"∙", clsBin}, "oplus": {"⊕", clsBin}, "ominus": {"⊖", clsBin},
	"otimes": {"⊗", clsBin}, "odot": {"⊙", clsBin}, "cup": {"∪", clsBin}, "cap": {"∩", clsBin}, "setminus": {"∖", clsBin},
	"wedge": {"∧", clsBin}, "land": {"∧", clsBin}, "vee": {"∨", clsBin}, "lor": {"∨", clsBin}, "sqcup": {"⊔", clsBin}, "sqcap": {"⊓", clsBin},
	"uplus": {"⊎", clsBin}, "wr": {"≀", clsBin}, "diamond": {"⋄", clsBin},
	// relations
	"leq": {"≤", clsRel}, "le": {"≤", clsRel}, "geq": {"≥", clsRel}, "ge": {"≥", clsRel}, "neq": {"≠", clsRel}, "ne": {"≠", clsRel},
	"approx": {"≈", clsRel}, "equiv": {"≡", clsRel}, "sim": {"∼", clsRel}, "simeq": {"≃", clsRel}, "cong": {"≅", clsRel},
	"propto": {"∝", clsRel}, "ll": {"≪", clsRel}, "gg": {"≫", clsRel}, "prec": {"≺", clsRel}, "succ": {"≻", clsRel},
	"in": {"∈", clsRel}, "notin": {"∉", clsRel}, "ni": {"∋", clsRel}, "subset": {"⊂", clsRel}, "supset": {"⊃", clsRel},
	"subseteq": {"⊆", clsRel}, "supseteq": {"⊇", clsRel}, "subsetneq": {"⊊", clsRel}, "mid": {"∣", clsRel}, "parallel": {"∥", clsRel},
	"perp": {"⊥", clsRel}, "models": {"⊨", clsRel}, "vdash": {"⊢", clsRel}, "dashv": {"⊣", clsRel},
	"to": {"→", clsRel}, "rightarrow": {"→", clsRel}, "leftarrow": {"←", clsRel}, "gets": {"←", clsRel},
	"leftrightarrow": {"↔", clsRel}, "Rightarrow": {"⇒", clsRel}, "Leftarrow": {"⇐", clsRel}, "Leftrightarrow": {"⇔", clsRel},
	"implies": {"⟹", clsRel}, "impliedby": {"⟸", clsRel}, "iff": {"⟺", clsRel}, "mapsto": {"↦", clsRel},
	"longrightarrow": {"⟶", clsRel}, "longleftarrow": {"⟵", clsRel}, "Longrightarrow": {"⟹", clsRel},
	"uparrow": {"↑", clsRel}, "downarrow": {"↓", clsRel}, "updownarrow": {"↕", clsRel}, "nearrow": {"↗", clsRel}, "searrow": {"↘", clsRel},
	"hookrightarrow": {"↪", clsRel}, "rightleftharpoons": {"⇌", clsRel}, "coloneqq": {"≔", clsRel}, "doteq": {"≐", clsRel},
	"asymp": {"≍", clsRel}, "nless": {"≮", clsRel}, "ngtr": {"≯", clsRel}, "lesssim": {"≲", clsRel}, "gtrsim": {"≳", clsRel},
	// punctuation
	"colon": {":", clsPunct},
}

// Large operators; limits go above/below in display style when limits is true.
var bigOps = map[string]struct {
	s      string
	limits bool
}{
	"sum": {"∑", true}, "prod": {"∏", true}, "coprod": {"∐", true}, "bigcup": {"⋃", true}, "bigcap": {"⋂", true},
	"bigoplus": {"⨁", true}, "bigotimes": {"⨂", true}, "bigvee": {"⋁", true}, "bigwedge": {"⋀", true}, "bigsqcup": {"⨆", true},
	"int": {"∫", false}, "iint": {"∬", false}, "iiint": {"∭", false}, "oint": {"∮", false},
}

// Named operators rendered upright; true when they take limits in display style.
var namedOps = map[string]bool{
	"lim": true, "limsup": true, "liminf": true, "max": true, "min": true, "sup": true, "inf": true, "det": true,
	"gcd": true, "Pr": true, "argmax": true, "argmin": true,
	"sin": false, "cos": false, "tan": false, "cot": false, "sec": false, "csc": false, "arcsin": false, "arccos": false,
	"arctan": false, "sinh": false, "cosh": false, "tanh": false, "coth": false, "log": false, "ln": false, "lg": false, "exp": false,
	"ker": false, "dim": false, "deg": false, "hom": false, "arg": false, "mod": false, "bmod": false,
}

var accents = map[string]string{
	"hat": "ˆ", "widehat": "ˆ", "check": "ˇ", "tilde": "˜", "widetilde": "˜", "acute": "´", "grave": "`",
	"dot": "˙", "ddot": "¨", "breve": "˘", "bar": "¯", "vec": "→", "mathring": "˚",
}

var colors = map[string]string{
	"red": "#e06c75", "green": "#98c379", "blue": "#61afef", "yellow": "#e5c07b", "orange": "#d19a66",
	"purple": "#c678dd", "magenta": "#c678dd", "cyan": "#56b6c2", "gray": "#888888", "grey": "#888888",
	"black": "#000000", "white": "#ffffff", "brown": "#a0522d", "pink": "#ff79c6", "teal": "#56b6c2",
}

// Unicode alphabets for \mathbb, \mathcal, \mathfrak, \mathscr.
func mapAlphabet(font string, r rune) rune {
	exceptions := map[string]map[rune]rune{
		"bb":   {'C': 'ℂ', 'H': 'ℍ', 'N': 'ℕ', 'P': 'ℙ', 'Q': 'ℚ', 'R': 'ℝ', 'Z': 'ℤ'},
		"cal":  {'B': 'ℬ', 'E': 'ℰ', 'F': 'ℱ', 'H': 'ℋ', 'I': 'ℐ', 'L': 'ℒ', 'M': 'ℳ', 'R': 'ℛ', 'e': 'ℯ', 'g': 'ℊ', 'o': 'ℴ'},
		"frak": {'C': 'ℭ', 'H': 'ℌ', 'I': 'ℑ', 'R': 'ℜ', 'Z': 'ℨ'},
	}
	if font == "scr" {
		font = "cal"
	}
	if m, ok := exceptions[font]; ok {
		if x, ok := m[r]; ok {
			return x
		}
	}
	base := map[string][2]rune{"bb": {0x1D538, 0x1D552}, "cal": {0x1D49C, 0x1D4B6}, "frak": {0x1D504, 0x1D51E}}[font]
	switch {
	case base[0] == 0:
		return r
	case r >= 'A' && r <= 'Z':
		return base[0] + (r - 'A')
	case r >= 'a' && r <= 'z':
		return base[1] + (r - 'a')
	case font == "bb" && r >= '0' && r <= '9':
		return 0x1D7D8 + (r - '0')
	}
	return r
}

// advance estimates the advance width of r in em, for a serif text face.
func advance(r rune, italic bool) float64 {
	w := 0.5
	switch {
	case r >= 'a' && r <= 'z':
		w = map[rune]float64{'a': .5, 'b': .53, 'c': .45, 'd': .54, 'e': .48, 'f': .36, 'g': .5, 'h': .55, 'i': .3, 'j': .3,
			'k': .52, 'l': .3, 'm': .83, 'n': .56, 'o': .52, 'p': .54, 'q': .53, 'r': .4, 's': .42, 't': .34, 'u': .56,
			'v': .5, 'w': .74, 'x': .5, 'y': .5, 'z': .45}[r]
	case r >= 'A' && r <= 'Z':
		w = map[rune]float64{'I': .38, 'J': .38, 'M': .88, 'W': .96, 'L': .6, 'F': .6, 'P': .6, 'E': .64, 'S': .55}[r]
		if w == 0 {
			w = .7
		}
	case r >= '0' && r <= '9':
		w = .55
	case r == ' ':
		w = .26
	case r == '(' || r == ')' || r == '[' || r == ']':
		w = .35
	case r == '{' || r == '}':
		w = .45
	case r == '|' || r == '∣':
		w = .25
	case r == ',' || r == '.' || r == ';' || r == ':' || r == '!' || r == '\'' || r == '′':
		w = .27
	case r == '/' || r == '∖':
		w = .4
	case r >= 0x391 && r <= 0x3A9: // upper greek
		w = .68
	case r >= 0x3B1 && r <= 0x3D6: // lower greek
		w = .55
	case r == '+' || r == '=' || r == '<' || r == '>' || r == '−' || r == '±' || r == '×' || r == '÷' || r == '≤' || r == '≥' || r == '≠' || r == '≈' || r == '≡' || r == '∼':
		w = .62
	case r >= 0x2190 && r <= 0x21FF: // arrows
		w = .9
	case r >= 0x27F5 && r <= 0x27FF: // long arrows
		w = 1.5
	case r == '∞':
		w = .8
	case r == '…' || r == '⋯':
		w = .9
	case r == '⋅' || r == '∙':
		w = .28
	case r >= 0x1D400: // math alphanumerics
		w = .7
	case r >= 0x2200 && r <= 0x22FF:
		w = .65
	}
	if italic {
		w *= 0.97
	}
	return w * 1.05
}

// glyph vertical extents in em: height above and depth below the baseline.
func extents(r rune) (float64, float64) {
	switch {
	case r >= 'a' && r <= 'z':
		h := .47
		if r == 'b' || r == 'd' || r == 'f' || r == 'h' || r == 'k' || r == 'l' || r == 't' || r == 'i' || r == 'j' {
			h = .72
		}
		d := 0.0
		if r == 'g' || r == 'j' || r == 'p' || r == 'q' || r == 'y' || r == 'f' {
			d = .22
		}
		return h, d
	case r == '(' || r == ')' || r == '[' || r == ']' || r == '{' || r == '}' || r == '|':
		return .76, .26
	case r == ',' || r == ';':
		return .1, .18
	case r == '.' || r == '…':
		return .1, 0
	case r == '⋅' || r == '⋯' || r == '−' || r == '+' || r == '=' || r == '×':
		return .55, .05
	case r >= 0x3B1 && r <= 0x3D6:
		if r == 'β' || r == 'δ' || r == 'ζ' || r == 'θ' || r == 'λ' || r == 'ξ' || r == 'ϕ' || r == 'ψ' {
			return .72, .22
		}
		return .47, .2
	}
	return .7, .02
}
