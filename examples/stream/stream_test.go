package stream

import (
	"context"
	"testing"

	"github.com/zkrebbekx/promptr"
	"github.com/zkrebbekx/promptr/providers/fake"

	. "github.com/smartystreets/goconvey/convey"
)

func TestSummarizeArticleStreams(t *testing.T) {
	Convey("Given a fake provider scripted to emit a Summary in small chunks", t, func() {
		p := fake.New(`{"headline": "Go ships generics", "bullets": ["type params", "constraints"]}`)
		p.ChunkSize = 6

		Convey("When SummarizeArticle streams it", func() {
			ch, err := SummarizeArticle(context.Background(), p, "long article text")
			So(err, ShouldBeNil)

			var snapshots int
			var last promptr.Partial[Summary]
			for part := range ch {
				snapshots++
				last = part
			}

			Convey("Then it yields several partial snapshots ending in a complete value", func() {
				So(snapshots, ShouldBeGreaterThan, 1)
				So(last.Err, ShouldBeNil)
				So(last.Complete, ShouldBeTrue)
				So(last.Value.Headline, ShouldEqual, "Go ships generics")
				So(last.Value.Bullets, ShouldResemble, []string{"type params", "constraints"})
			})
		})
	})
}

func TestCaptionImageBinds(t *testing.T) {
	Convey("Given a fake provider and an inline image part", t, func() {
		p := fake.New(`{"headline": "a red bicycle", "bullets": ["outdoors", "daytime"]}`)

		Convey("When CaptionImage is called with an image and a hint", func() {
			got, err := CaptionImage(context.Background(), p, promptr.ImagePart("image/png", []byte{1, 2, 3}), "focus on the object")
			So(err, ShouldBeNil)

			Convey("Then the typed Summary is parsed from the reply", func() {
				So(got.Headline, ShouldEqual, "a red bicycle")
				So(got.Bullets, ShouldResemble, []string{"outdoors", "daytime"})
			})
		})
	})
}
