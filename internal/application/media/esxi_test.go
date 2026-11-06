package media_test

import "testing"

func TestIdentifyESXiBuildFromMedia(t *testing.T) {
	for _, version := range []string{"7.0.3-0.135.24585291", "8.0.3-0.102.25595708", "9.0.2-0100.25595025"} {
		t.Run(version, func(t *testing.T) {
			got, err := Identify(t.Context(), identifyTree(map[string]string{
				"boot.cfg":             "title=Loading ESXi installer\nbuild=" + version + "\n",
				"efi/boot/bootx64.efi": "loader",
			}))
			if err != nil || got.Series != "esxi" || got.Format != "esxi-ks" || got.Version != version || got.Arch != "x86_64" {
				t.Fatalf("识别结果：%+v，错误：%v", got, err)
			}
		})
	}
}

func TestIdentifyESXiRejectsUnreliableBuild(t *testing.T) {
	for _, raw := range []string{
		"title=ESXi 8.0.3\n", "build=../8.0.3\n", "build=8.0.3\n",
		"build=8.0.3-0.102.25595708\nbuild=7.0.3-0.135.24585291\n",
		"build=10.0.0-0.1.12345\n", "build=8.0.3-0.102.25595708\x00\n",
	} {
		_, err := Identify(t.Context(), identifyTree(map[string]string{"boot.cfg": raw, "efi/boot/bootx64.efi": "loader"}))
		identifyCode(t, err, "iso_version_missing")
	}
}
