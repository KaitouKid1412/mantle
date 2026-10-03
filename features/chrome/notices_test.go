package chrome

import (
	"testing"
	"time"

	"github.com/KaitouKid1412/mantle/pkg/ext"
	"github.com/KaitouKid1412/mantle/pkg/ext/exttest"
	"github.com/KaitouKid1412/mantle/pkg/proto"
)

func TestEngineNotices(t *testing.T) {
	ctx := exttest.NewCtx()
	engineNotice(ctx, ev(&proto.Notification{Key: "k", Text: "MCP server reconnected", Color: "warning", TimeoutMS: 3000}))
	engineNotice(ctx, ev(&proto.Notification{Text: "  "}))
	engineNotice(ctx, ev(&proto.Status{}))
	if len(ctx.Notices) != 1 {
		t.Fatalf("notices = %+v", ctx.Notices)
	}
	n := ctx.Notices[0]
	if n.Key != "engine:main:k" || n.Level != ext.NoticeWarning || n.Timeout != 3*time.Second || n.Source != "main" {
		t.Errorf("notice = %+v", n)
	}
	if noticeLevel("high", "") != ext.NoticeWarning || noticeLevel("", "") != ext.NoticeInfo ||
		noticeLevel("", "red") != ext.NoticeError || noticeLevel("", "green") != ext.NoticeSuccess {
		t.Error("levels")
	}
}
