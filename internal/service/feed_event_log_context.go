package service

import "context"

type feedEventLogFields struct {
	StreamID     string
	AuthorUserID int64
	PostID       int64
}

type feedEventLogContextKey struct{}

func withFeedEventLogFields(ctx context.Context, fields feedEventLogFields) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithValue(ctx, feedEventLogContextKey{}, fields)
}

func getFeedEventLogFields(ctx context.Context) feedEventLogFields {
	if ctx == nil {
		return feedEventLogFields{}
	}
	fields, _ := ctx.Value(feedEventLogContextKey{}).(feedEventLogFields)
	return fields
}
