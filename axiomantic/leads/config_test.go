package main

import "testing"

func TestNormLoginDropsInvisible(t *testing.T) {
	for _, in := range []string{"\ufeffOwner@Studio.ru", "owner@studio.ru\u200b", " \xffowner@studio.ru"} {
		if got := normLogin(in); got != "owner@studio.ru" {
			t.Errorf("normLogin(%q) = %q", in, got)
		}
	}
}
