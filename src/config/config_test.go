package config

import "testing"

func TestNewSetConfig_DeepCopy(t *testing.T) {
	set1 := NewSetConfig()
	set2 := NewSetConfig()

	set1.TCP.Win.Values = append(set1.TCP.Win.Values, 999)
	set1.Targets.GeoSiteCategories = append(set1.Targets.GeoSiteCategories, "test")
	set1.Faking.SNIMutation.FakeSNIs = append(set1.Faking.SNIMutation.FakeSNIs, "test.com")

	if len(set2.TCP.Win.Values) != 4 {
		t.Error("WinValues leaked between instances")
	}
	if len(set2.Targets.GeoSiteCategories) != 0 {
		t.Error("GeoSiteCategories leaked between instances")
	}
	if len(set2.Faking.SNIMutation.FakeSNIs) != 3 {
		t.Error("FakeSNIs leaked between instances")
	}
}
