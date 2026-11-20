package report

import (
	"reflect"

	"rollproof/internal/mask"
)

// Mask returns a copy of r in which m has masked every string, those of lists
// and pointers included, except map keys and the fields tagged mask:"keep",
// which hold a fixed vocabulary; times and numbers are copied as they are.
// r is not changed. The console prints the copy; Final writes it.
func Mask(r Run, m *mask.Masker) Run {
	return masked(reflect.ValueOf(r), m).Interface().(Run)
}

// pkgPath is this package's path: only the structs it declares hold free text;
// a time.Time or a struct of another package is copied as it is.
var pkgPath = reflect.TypeOf(Run{}).PkgPath()

// masked returns v with each of its strings masked by m, walking the structs of
// this package field by field, a list and a pointer with their contents, and a
// map with its values, its keys kept. It never writes to v.
func masked(v reflect.Value, m *mask.Masker) reflect.Value {
	switch v.Kind() {
	case reflect.String:
		return reflect.ValueOf(m.String(v.String()))
	case reflect.Pointer:
		if v.IsNil() {
			return v
		}
		out := reflect.New(v.Type().Elem())
		out.Elem().Set(masked(v.Elem(), m))
		return out
	case reflect.Slice:
		if v.IsNil() {
			return v
		}
		out := reflect.MakeSlice(v.Type(), v.Len(), v.Len())
		for i := 0; i < v.Len(); i++ {
			out.Index(i).Set(masked(v.Index(i), m))
		}
		return out
	case reflect.Map:
		if v.IsNil() {
			return v
		}
		out := reflect.MakeMapWithSize(v.Type(), v.Len())
		iter := v.MapRange()
		for iter.Next() {
			out.SetMapIndex(iter.Key(), masked(iter.Value(), m))
		}
		return out
	case reflect.Struct:
		if v.Type().PkgPath() != pkgPath {
			return v
		}
		out := reflect.New(v.Type()).Elem()
		t := v.Type()
		for i := 0; i < t.NumField(); i++ {
			if t.Field(i).Tag.Get("mask") == "keep" {
				out.Field(i).Set(masked(v.Field(i), nil))
				continue
			}
			out.Field(i).Set(masked(v.Field(i), m))
		}
		return out
	}
	return v
}
