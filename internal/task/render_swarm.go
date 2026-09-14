package task

func renderSwarm(t Task) ([]byte, error) {
	services := make(orderedMap, 0, len(t.Services))
	for _, s := range t.Services {
		services = append(services, kv{s.Name, composeService(s, true)})
	}
	doc := orderedMap{
		{"name", t.Name},
		{"services", services},
	}
	if nets := composeNetworks(t, true); len(nets) > 0 {
		doc = append(doc, kv{"networks", nets})
	} else {
		doc = append(doc, kv{"networks", orderedMap{
			{"default", orderedMap{{"driver", "overlay"}}},
		}})
	}
	if vols := composeVolumes(t); len(vols) > 0 {
		doc = append(doc, kv{"volumes", vols})
	}
	return marshalYAML(doc)
}
