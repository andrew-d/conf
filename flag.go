package conf

import (
	"io/ioutil"
	"strings"

	flag "github.com/spf13/pflag"
)

func newFlagSet(cfg Map, name string, sources ...Source) *flag.FlagSet {
	set := flag.NewFlagSet(name, flag.ContinueOnError)
	set.SetOutput(ioutil.Discard)

	cfg.Scan(func(path []string, item MapItem) {
		flag := set.VarPF(item.Value, strings.Join(append(path, item.Name), "."), "", item.Help)

		// In order to get standalone bool flags working, need to set
		// 'NoOptDefVal' to true - this means that specifying the flag
		// with no value will parse as 'true'
		if _, ok := item.Value.Value().(bool); ok {
			flag.NoOptDefVal = "true"
		}
	})

	for _, source := range sources {
		if f, ok := source.(FlagSource); ok {
			set.Var(f, f.Flag(), f.Help())
		}
	}

	return set
}
