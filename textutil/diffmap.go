package textutil

import (
	"fmt"
	"regexp"
	"sort"
	"strings"

	"github.com/flanksource/clicky"
	"github.com/flanksource/clicky/api"
)

var (
	sha256Regex = regexp.MustCompile(`[a-fA-F0-9]{64}`)
	sopsRegex   = regexp.MustCompile(`ENC\[([^,]+),data:([^,]+),iv:[^,]+,tag:[^,]+,type:([^\]]+)\]`)
)

type DiffMap[T any] map[string]T

func flattenSlice[T any](s []T, prefix string) DiffMap[T] {
	result := make(DiffMap[T])

	for i, value := range s {
		fullKey := fmt.Sprintf("%s[%d]", prefix, i)

		if nestedMap, ok := any(value).(map[string]any); ok {
			for k, v := range flattenMap(nestedMap, fullKey) {
				if v != nil {
					result[k] = any(v).(T)
				} else {
					result[k] = *new(T)
				}
			}
		} else if nestedSlice, ok := any(value).([]any); ok {
			for j, v := range nestedSlice {
				if v != nil {
					result[fmt.Sprintf("%s[%d]", fullKey, j)] = any(v).(T)
				} else {
					result[fmt.Sprintf("%s[%d]", fullKey, j)] = *new(T)
				}
			}
		} else {
			result[fullKey] = value
		}
	}

	return result
}

func flattenMap[T any](m map[string]T, prefix string) DiffMap[T] {
	result := make(DiffMap[T])

	for key, value := range m {
		fullKey := key
		if prefix != "" {
			fullKey = prefix + "." + key
		}

		if nestedMap, ok := any(value).(map[string]any); ok {
			for k, v := range flattenMap(nestedMap, fullKey) {
				if v != nil {
					result[k] = any(v).(T)
				} else {
					result[k] = *new(T)
				}
			}
		} else if nestedSlice, ok := any(value).([]any); ok {
			for i, v := range flattenSlice(nestedSlice, fullKey) {
				if v != nil {
					result[i] = any(v).(T)
				} else {
					result[i] = *new(T)
				}
			}
		} else {
			result[fullKey] = value
		}
	}

	return result
}

func CompactSHA256(s string) string {
	return sha256Regex.ReplaceAllStringFunc(s, func(match string) string {
		if len(match) != 64 {
			return match
		}
		return match[:2] + ".." + match[len(match)-2:]
	})
}

func CompactSOPS(s string) string {
	return sopsRegex.ReplaceAllStringFunc(s, func(match string) string {
		matches := sopsRegex.FindStringSubmatch(match)
		if len(matches) != 4 {
			return match
		}

		encType := matches[1]
		data := matches[2]
		valueType := matches[3]

		dataPrefix := data
		if len(data) > 3 {
			dataPrefix = data[:3]
		}

		return fmt.Sprintf("ENC[%s,data:%s..%d more chars,type:%s]", encType, dataPrefix, len(data), valueType)
	})
}

func FindCommonPrefix(a, b string) string {
	minLen := len(a)
	if len(b) < minLen {
		minLen = len(b)
	}

	for i := 0; i < minLen; i++ {
		if a[i] != b[i] {
			return a[:i]
		}
	}

	return a[:minLen]
}

func FindCommonSuffix(a, b string) string {
	lenA, lenB := len(a), len(b)
	minLen := lenA
	if lenB < minLen {
		minLen = lenB
	}

	for i := 0; i < minLen; i++ {
		if a[lenA-1-i] != b[lenB-1-i] {
			if i == 0 {
				return ""
			}
			return a[lenA-i:]
		}
	}

	return a[lenA-minLen:]
}

func HumanDiff(oldVal, newVal string) api.Text {
	oldVal = CompactSHA256(oldVal)
	newVal = CompactSHA256(newVal)
	oldVal = CompactSOPS(oldVal)
	newVal = CompactSOPS(newVal)

	prefix := FindCommonPrefix(oldVal, newVal)
	suffix := FindCommonSuffix(oldVal, newVal)

	if len(prefix)+len(suffix) > len(oldVal) || len(prefix)+len(suffix) > len(newVal) {
		suffix = ""
	}

	oldDiff := oldVal[len(prefix) : len(oldVal)-len(suffix)]
	newDiff := newVal[len(prefix) : len(newVal)-len(suffix)]

	t := clicky.Text("")

	if prefix != "" {
		t = t.Append(prefix, "text-muted")
	}

	if oldDiff != "" {
		t = t.Append(oldDiff, "text-red-500")
	}

	if suffix != "" && oldDiff != "" {
		t = t.Append(suffix, "text-muted")
	}

	t = t.Append(" → ", "text-muted")

	if newDiff != "" {
		t = t.Append(newDiff, "text-green-500")
	}

	if suffix != "" && newDiff != "" {
		t = t.Append(suffix, "text-muted")
	}

	return t
}

func (d DiffMap[T]) Diff(other DiffMap[T]) DiffMap[api.Text] {
	changes := make(DiffMap[api.Text])

	flatThis := flattenMap(d, "")
	flatOther := flattenMap(other, "")

	allKeys := make(map[string]bool)
	for k := range flatThis {
		allKeys[k] = true
	}
	for k := range flatOther {
		allKeys[k] = true
	}

	for key := range allKeys {
		thisVal, inThis := flatThis[key]
		otherVal, inOther := flatOther[key]

		if inThis && !inOther {
			changes[key] = clicky.Text("-", "text-red-500").Append(thisVal, "strikethrough text-red-500")
		} else if !inThis && inOther {
			changes[key] = clicky.Text("").Append(otherVal, "text-green-500")
		} else if fmt.Sprintf("%v", thisVal) != fmt.Sprintf("%v", otherVal) {
			thisStr, thisIsString := any(thisVal).(string)
			otherStr, otherIsString := any(otherVal).(string)

			if thisIsString && otherIsString {
				changes[key] = HumanDiff(thisStr, otherStr)
			} else {
				changes[key] = clicky.Text("").Append(thisVal, "text-red-500").Append(" → ").Append(otherVal, "text-green-500").Append(" (type: " + fmt.Sprintf("%T", otherVal) + ")")
			}
		}
	}

	return changes
}

func smartCollapse(m map[string]any) map[string]any {
	result := make(map[string]any)

	for key, value := range m {
		nestedMap, isMap := value.(map[string]any)
		if !isMap {
			result[key] = value
			continue
		}

		collapsed := smartCollapse(nestedMap)

		if len(collapsed) == 1 {
			for childKey, childValue := range collapsed {
				result[key+"."+childKey] = childValue
			}
		} else {
			result[key] = collapsed
		}
	}

	return result
}

func (d DiffMap[T]) Collapse() map[string]any {
	result := make(map[string]any)

	for key, value := range d {
		parts := strings.Split(key, ".")

		current := result
		for i, part := range parts {
			if i == len(parts)-1 {
				current[part] = value
			} else {
				if _, exists := current[part]; !exists {
					current[part] = make(map[string]any)
				}
				switch current[part].(type) {
				case map[string]any:
					current = current[part].(map[string]any)
				default:
					current[part] = make(map[string]any)
				}
			}
		}
	}

	return smartCollapse(result)
}

func (d DiffMap[T]) Pretty() api.Text {
	collapsed := d.Collapse()
	return RenderMapAsYAML(collapsed, 0)
}

func RenderMapAsYAML(m map[string]any, indent int) api.Text {
	t := clicky.Text("")
	if len(m) == 0 {
		return t
	}

	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	var indentT api.Text
	for i := 0; i < indent; i++ {
		indentT = indentT.Tab()
	}

	for _, key := range keys {
		value := m[key]

		switch v := value.(type) {
		case map[string]any:
			t = t.Append(indentT).Append(key).Append(": ", "text-muted").NewLine()
			t = t.Append(RenderMapAsYAML(v, indent+1))
		default:
			t = t.Append(indentT).Append(key).Append(": ", "text-muted").Append(v, "max-w-[100ch]").NewLine()
		}
	}

	return t
}
