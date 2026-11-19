package config

import "sort"

// NotBuilt lists the keys of this configuration that this build cannot run yet, such as
// registries, sorted. It is empty when the build can run all of it. The
// configuration is valid either way.
func (c *Config) NotBuilt() []string {
	var keys []string
	if len(c.Registries) > 0 {
		keys = append(keys, "registries")
	}
	for name, im := range c.Images {
		if im.As != "" {
			keys = append(keys, "images."+name+".as")
		}
	}
	sort.Strings(keys)
	return keys
}
