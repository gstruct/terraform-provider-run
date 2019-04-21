package provider

import "fmt"

func stringInSlice(a string, list []string) bool {
	for _, b := range list {
		if b == a {
			return true
		}
	}
	return false
}

func map_if_to_str(a map[string]interface{}) map[string]string {
	b := make(map[string]string)

	for k, v := range a {
		sk := fmt.Sprintf("%v", k)
		sv := fmt.Sprintf("%v", v)

		b[sk] = sv
	}

	return b
}
