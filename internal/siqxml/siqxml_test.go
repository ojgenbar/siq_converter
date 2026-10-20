package siqxml_test

import (
	"os"
	"reflect"
	"strings"
	"testing"

	"siq_converter/internal/siqxml"
)

func TestEncodeIsCanonicalSIGameForm(t *testing.T) {
	src, err := os.ReadFile("../../testdata/full.xml")
	if err != nil {
		t.Fatal(err)
	}
	p, err := siqxml.Decode(src)
	if err != nil {
		t.Fatal(err)
	}
	out := siqxml.Encode(p, []siqxml.FileHash{{Name: "Images/a.png", Hash: "AB"}})
	s := string(out)
	for _, want := range []string{
		"\xEF\xBB\xBF<?xml version=\"1.0\" encoding=\"utf-8\"?><package name=\"Full &amp; &quot;fixture&quot;\" version=\"5\" id=",
		`language="en-US" generator="hand" xmlns="https://github.com/VladimirKhil/SI/blob/master/assets/siq_5.xsd">`,
		`</global><files><file name="Images/a.png" hash="AB" /></files><info>`,
		`<item type="image" isRef="True" waitForFinish="False">a b+c@2x (1).jpg</item>`,
		`<item type="audio" isRef="True" placement="background">song.mp3</item>`,
		`<item type="audio" isRef="True" placement="screen" duration="00:00:05">клип.mp3</item>`,
		`<item type="image" isRef="True">explicit default.png</item>`,
		`<param name="price" type="numberSet"><numberSet minimum="100" maximum="500" step="100" /></param>`,
		`<right></right>`,
		`<round name=""></round>`,
	} {
		if !strings.Contains(s, want) {
			t.Errorf("output lacks %q", want)
		}
	}
	if strings.Contains(s, "DEADBEEF") {
		t.Error("stale <files> entry was kept")
	}
	if strings.Contains(s, "\n") && !strings.Contains(s, "Line one\nLine two") {
		t.Error("unexpected newlines in output")
	}

	back, err := siqxml.Decode(out)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(p, back) {
		t.Error("model changed after XML round trip")
	}
}

func TestDecodeRejectsUnknownData(t *testing.T) {
	head := `<package name="x" version="5"><rounds><round name="r"><themes><theme name="t"><questions>`
	tail := `</questions></theme></themes></round></rounds></package>`
	cases := map[string]string{
		"unknown attribute": `<package name="x" version="5" color="red" />`,
		"unknown element":   `<package name="x" version="5"><bonus /></package>`,
		"v4 scenario":       head + `<question price="1"><scenario><atom>q</atom></scenario><right /></question>` + tail,
		"v4 version":        `<package name="x" version="4" />`,
		"bad bool":          head + `<question price="1"><params><param name="q" type="content"><item isRef="maybe">a</item></param></params><right /></question>` + tail,
		"duplicate param":   head + `<question price="1"><params><param name="a">1</param><param name="a">2</param></params><right /></question>` + tail,
		"text in content":   head + `<question price="1"><params><param name="q" type="content">stray<item>a</item></param></params><right /></question>` + tail,
	}
	for name, src := range cases {
		if _, err := siqxml.Decode([]byte(src)); err == nil {
			t.Errorf("%s: expected an error", name)
		}
	}
}
