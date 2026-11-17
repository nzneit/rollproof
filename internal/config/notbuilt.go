package config

import "sort"

// NotBuilt lists the keys of this configuration that this build cannot run yet, such as
// registries or probe.custom, sorted. It is empty when the build can run all of it. The
// configuration is valid either way.
func (c *Config) NotBuilt() []string {
	var keys []string
	if len(c.Registries) > 0 {
		keys = append(keys, "registries")
	}
	if len(c.Setup) > 0 {
		keys = append(keys, "setup")
	}
	if c.Deploy.Commands != nil {
		keys = append(keys, "deploy.commands")
	}
	if h := c.Deploy.Helm; h != nil && h.Previous == nil {
		keys = append(keys, "deploy.helm.previous")
	}
	if c.Probe.Custom != nil {
		keys = append(keys, "probe.custom")
	}
	for name, im := range c.Images {
		if im.Archive != "" {
			keys = append(keys, "images."+name+".archive")
		}
		if im.As != "" {
			keys = append(keys, "images."+name+".as")
		}
	}
	sort.Strings(keys)
	return keys
}
