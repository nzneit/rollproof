package config

import "sort"

// NotBuilt lists the keys of this configuration that this build cannot run yet, sorted: it lists
// images.NAME.as alone now; registries is built. It is empty when the build can run all of it. The
// configuration is valid either way.
func (c *Config) NotBuilt() []string {
	var keys []string
	for name, im := range c.Images {
		if im.As != "" {
			keys = append(keys, "images."+name+".as")
		}
	}
	sort.Strings(keys)
	return keys
}
