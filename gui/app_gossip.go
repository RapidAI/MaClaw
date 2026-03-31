package main

import (
	"context"
	"time"
)

// GossipPublish 发布八卦帖子（Wails 绑定）。
func (a *App) GossipPublish(content, category string) (*GossipPublishResult, error) {
	a.ensureGossipClient()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	return a.gossipClient.PublishPost(ctx, content, category)
}

// GossipBrowse 浏览帖子列表（Wails 绑定）。
func (a *App) GossipBrowse(page int) (*GossipBrowseResult, error) {
	a.ensureGossipClient()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	return a.gossipClient.BrowsePosts(ctx, page)
}

// GossipComment 提交评论（Wails 绑定）。
func (a *App) GossipComment(postID, content string, rating int) (*GossipCommentResult, error) {
	a.ensureGossipClient()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	return a.gossipClient.AddComment(ctx, postID, content, rating)
}

// GossipRate 评分帖子（Wails 绑定）。
func (a *App) GossipRate(postID string, rating int) error {
	a.ensureGossipClient()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	return a.gossipClient.RatePost(ctx, postID, rating)
}

// GossipGetComments 获取帖子评论列表（Wails 绑定）。
func (a *App) GossipGetComments(postID string, page int) (*GossipCommentsResult, error) {
	a.ensureGossipClient()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	return a.gossipClient.GetComments(ctx, postID, page)
}

// GossipSnapshot 获取快照数据，支持 ETag 缓存（Wails 绑定）。
func (a *App) GossipSnapshot(etag string) (*GossipSnapshotResult, error) {
	a.ensureGossipClient()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	return a.gossipClient.GetSnapshot(ctx, etag)
}
