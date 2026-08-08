package convert

import "github.com/daniellavrushin/b4/config"

type normalizer func(*Program, []Token, *noteSet)

type toolEmitter func(*config.SetConfig, *Profile, tokenIndex, *noteSet)

var (
	normalizers  = map[string]normalizer{}
	toolEmitters = map[string]toolEmitter{}
)

func registerNormalizer(name string, fn normalizer) { normalizers[name] = fn }

func registerToolEmitter(tool string, fn toolEmitter) { toolEmitters[tool] = fn }

func runNormalizer(name string, prog *Program, tokens []Token, notes *noteSet) {
	if fn, ok := normalizers[name]; ok {
		fn(prog, tokens, notes)
	}
}

func runToolEmitter(tool string, set *config.SetConfig, prof *Profile, ti tokenIndex, notes *noteSet) {
	if fn, ok := toolEmitters[tool]; ok {
		fn(set, prof, ti, notes)
	}
}
