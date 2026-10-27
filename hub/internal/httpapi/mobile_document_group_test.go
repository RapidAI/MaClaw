package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/hub/internal/auth"
)

func TestCompanionDocumentGroupShareSyncAndRemoval(t *testing.T) {
	resetCompanionDocShares(t)
	identity, _, _ := newHTTPAPITestServices(t)
	ownerToken, _ := issueViewerToken(t, identity, "group-owner@example.com")
	readerToken, _ := issueViewerToken(t, identity, "group-reader@example.com")
	otherToken, _ := issueViewerToken(t, identity, "group-other@example.com")

	create := httptest.NewRequest(http.MethodPost, "/api/mobile/documents/groups", strings.NewReader(`{"name":"论文组"}`))
	create.Header.Set("Authorization", "Bearer "+ownerToken)
	createRec := httptest.NewRecorder()
	CompanionDocumentGroupsHandler(identity).ServeHTTP(createRec, create)
	if createRec.Code != http.StatusOK || !strings.Contains(createRec.Body.String(), "论文组") {
		t.Fatalf("create group: %d %s", createRec.Code, createRec.Body.String())
	}
	var created map[string]any
	if err := json.Unmarshal(createRec.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}
	groupID, _ := created["id"].(string)
	if !companionGroupTokenPattern.MatchString(groupID) || created["admin"] != true {
		t.Fatalf("group: %s", createRec.Body.String())
	}

	add := httptest.NewRequest(http.MethodPost, "/api/mobile/documents/groups/"+groupID+"/members", strings.NewReader(`{"contact":"Group-Reader@example.com"}`))
	add.SetPathValue("id", groupID)
	add.Header.Set("Authorization", "Bearer "+ownerToken)
	addRec := httptest.NewRecorder()
	CompanionDocumentGroupMembersHandler(identity).ServeHTTP(addRec, add)
	if addRec.Code != http.StatusOK || !strings.Contains(addRec.Body.String(), `"status":"pending"`) || !strings.Contains(addRec.Body.String(), "group-reader@example.com") {
		t.Fatalf("add: %d %s", addRec.Code, addRec.Body.String())
	}

	if body := listCompanionInvites(t, identity, readerToken); !strings.Contains(body, groupID) || !strings.Contains(body, "论文组") || !strings.Contains(body, `"invited_at":"`) {
		t.Fatalf("invites: %s", body)
	}

	shareRec := postCompanionShareAs(t, identity, ownerToken, "group", "", "0", groupID, "论文.pdf", []byte("%PDF-group"))
	if shareRec.Code != http.StatusOK {
		t.Fatalf("group share: %d %s", shareRec.Code, shareRec.Body.String())
	}
	var shared map[string]any
	if err := json.Unmarshal(shareRec.Body.Bytes(), &shared); err != nil {
		t.Fatal(err)
	}
	token, _ := shared["token"].(string)
	if shared["visibility"] != "group" || shared["revision"] != float64(1) {
		t.Fatalf("share view: %s", shareRec.Body.String())
	}
	if strings.Contains(shareRec.Body.String(), groupID) {
		t.Fatalf("public create leaked the group id: %s", shareRec.Body.String())
	}

	if body := listCompanionShares(t, identity, readerToken); strings.Contains(body, token) {
		t.Fatalf("pending member saw the share: %s", body)
	}

	accept := httptest.NewRequest(http.MethodPost, "/api/mobile/documents/groups/"+groupID+"/accept", nil)
	accept.SetPathValue("id", groupID)
	accept.Header.Set("Authorization", "Bearer "+readerToken)
	acceptRec := httptest.NewRecorder()
	CompanionDocumentGroupAcceptHandler(identity).ServeHTTP(acceptRec, accept)
	if acceptRec.Code != http.StatusOK {
		t.Fatalf("accept: %d %s", acceptRec.Code, acceptRec.Body.String())
	}
	if body := listCompanionShares(t, identity, readerToken); !strings.Contains(body, token) {
		t.Fatalf("accepted member missing the share: %s", body)
	}
	importRec := importCompanionShare(t, identity, readerToken, token, "")
	if importRec.Code != http.StatusOK || string(importRec.Body.Bytes()) != "%PDF-group" {
		t.Fatalf("member import: %d %s", importRec.Code, importRec.Body.String())
	}

	personRec := postCompanionShareAs(t, identity, ownerToken, "person", "", "0", "group-reader@example.com", "给个人.pdf", []byte("%PDF-person"))
	if personRec.Code != http.StatusOK {
		t.Fatalf("person share: %d %s", personRec.Code, personRec.Body.String())
	}
	var person map[string]any
	if err := json.Unmarshal(personRec.Body.Bytes(), &person); err != nil {
		t.Fatal(err)
	}
	personToken, _ := person["token"].(string)
	if strings.Contains(personRec.Body.String(), "group-reader@example.com") {
		t.Fatalf("public create leaked the target: %s", personRec.Body.String())
	}
	if body := listCompanionShares(t, identity, readerToken); !strings.Contains(body, personToken) {
		t.Fatalf("person recipient missing the share: %s", body)
	}
	if body := listCompanionShares(t, identity, otherToken); strings.Contains(body, personToken) || strings.Contains(body, token) {
		t.Fatalf("stranger saw a directed share: %s", body)
	}

	syncRec := syncCompanionShare(t, identity, ownerToken, token, []byte("%PDF-group-v2"))
	if syncRec.Code != http.StatusOK {
		t.Fatalf("sync: %d %s", syncRec.Code, syncRec.Body.String())
	}
	var synced map[string]any
	if err := json.Unmarshal(syncRec.Body.Bytes(), &synced); err != nil {
		t.Fatal(err)
	}
	if synced["revision"] != float64(2) {
		t.Fatalf("revision: %s", syncRec.Body.String())
	}
	again := importCompanionShare(t, identity, readerToken, token, "")
	if again.Code != http.StatusOK || string(again.Body.Bytes()) != "%PDF-group-v2" {
		t.Fatalf("synced import: %d %s", again.Code, again.Body.String())
	}
	strangerSync := syncCompanionShare(t, identity, readerToken, token, []byte("nope"))
	if strangerSync.Code != http.StatusForbidden || !strings.Contains(strangerSync.Body.String(), "只有分享者可以同步更新") {
		t.Fatalf("reader sync: %d %s", strangerSync.Code, strangerSync.Body.String())
	}

	remove := httptest.NewRequest(http.MethodDelete, "/api/mobile/documents/groups/"+groupID+"/members", strings.NewReader(`{"contact":"group-reader@example.com"}`))
	remove.SetPathValue("id", groupID)
	remove.Header.Set("Authorization", "Bearer "+ownerToken)
	removeRec := httptest.NewRecorder()
	CompanionDocumentGroupMembersHandler(identity).ServeHTTP(removeRec, remove)
	if removeRec.Code != http.StatusOK || strings.Contains(removeRec.Body.String(), "group-reader@example.com") {
		t.Fatalf("remove: %d %s", removeRec.Code, removeRec.Body.String())
	}
	if body := listCompanionShares(t, identity, readerToken); strings.Contains(body, token) {
		t.Fatalf("removed member still sees the group share: %s", body)
	}
	if body := listCompanionShares(t, identity, readerToken); !strings.Contains(body, personToken) {
		t.Fatalf("person share was dropped with the group: %s", body)
	}
	denied := importCompanionShare(t, identity, readerToken, token, "")
	if denied.Code != http.StatusForbidden || !strings.Contains(denied.Body.String(), "已不在群组") {
		t.Fatalf("removed import: %d %s", denied.Code, denied.Body.String())
	}
	if inviteAfter := listCompanionInvites(t, identity, readerToken); strings.Contains(inviteAfter, groupID) {
		t.Fatalf("removed member still invited: %s", inviteAfter)
	}
}

func TestCompanionDocumentShareNotesReachEveryoneWhoCanSeeTheShare(t *testing.T) {
	resetCompanionDocShares(t)
	identity, _, _ := newHTTPAPITestServices(t)
	ownerToken, _ := issueViewerToken(t, identity, "note-owner@example.com")
	readerToken, _ := issueViewerToken(t, identity, "note-reader@example.com")
	otherToken, _ := issueViewerToken(t, identity, "note-other@example.com")
	strangerToken, _ := issueViewerToken(t, identity, "note-stranger@example.com")

	create := httptest.NewRequest(http.MethodPost, "/api/mobile/documents/groups", strings.NewReader(`{"name":"批注组"}`))
	create.Header.Set("Authorization", "Bearer "+ownerToken)
	createRec := httptest.NewRecorder()
	CompanionDocumentGroupsHandler(identity).ServeHTTP(createRec, create)
	if createRec.Code != http.StatusOK {
		t.Fatalf("create group: %d %s", createRec.Code, createRec.Body.String())
	}
	var created map[string]any
	if err := json.Unmarshal(createRec.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}
	groupID, _ := created["id"].(string)
	for _, contact := range []string{"note-reader@example.com", "note-other@example.com"} {
		add := httptest.NewRequest(http.MethodPost, "/api/mobile/documents/groups/"+groupID+"/members", strings.NewReader(`{"contact":"`+contact+`"}`))
		add.SetPathValue("id", groupID)
		add.Header.Set("Authorization", "Bearer "+ownerToken)
		addRec := httptest.NewRecorder()
		CompanionDocumentGroupMembersHandler(identity).ServeHTTP(addRec, add)
		if addRec.Code != http.StatusOK {
			t.Fatalf("add %s: %d %s", contact, addRec.Code, addRec.Body.String())
		}
	}
	for _, viewer := range []string{readerToken, otherToken} {
		accept := httptest.NewRequest(http.MethodPost, "/api/mobile/documents/groups/"+groupID+"/accept", nil)
		accept.SetPathValue("id", groupID)
		accept.Header.Set("Authorization", "Bearer "+viewer)
		acceptRec := httptest.NewRecorder()
		CompanionDocumentGroupAcceptHandler(identity).ServeHTTP(acceptRec, accept)
		if acceptRec.Code != http.StatusOK {
			t.Fatalf("accept: %d %s", acceptRec.Code, acceptRec.Body.String())
		}
	}

	shareRec := postCompanionShareAs(t, identity, ownerToken, "group", "", "0", groupID, "论文.pdf", []byte("%PDF-notes"))
	if shareRec.Code != http.StatusOK {
		t.Fatalf("share: %d %s", shareRec.Code, shareRec.Body.String())
	}
	var shared map[string]any
	if err := json.Unmarshal(shareRec.Body.Bytes(), &shared); err != nil {
		t.Fatal(err)
	}
	token, _ := shared["token"].(string)
	if strings.Contains(shareRec.Body.String(), "方法部分") {
		t.Fatalf("create leaked a note: %s", shareRec.Body.String())
	}

	empty := postCompanionNote(t, identity, readerToken, token, "  ")
	if empty.Code != http.StatusBadRequest || !strings.Contains(empty.Body.String(), "请填写批注") {
		t.Fatalf("empty note: %d %s", empty.Code, empty.Body.String())
	}
	posted := postCompanionNote(t, identity, readerToken, token, "方法部分值得再看")
	if posted.Code != http.StatusOK || !strings.Contains(posted.Body.String(), "方法部分值得再看") || !strings.Contains(posted.Body.String(), "note-reader@example.com") {
		t.Fatalf("post note: %d %s", posted.Code, posted.Body.String())
	}
	if !strings.Contains(posted.Body.String(), `"notes_revision":1`) {
		t.Fatalf("revision: %s", posted.Body.String())
	}

	ownerList := listCompanionShares(t, identity, ownerToken)
	otherList := listCompanionShares(t, identity, otherToken)
	if !strings.Contains(ownerList, "方法部分值得再看") || !strings.Contains(otherList, "方法部分值得再看") {
		t.Fatalf("owner=%s other=%s", ownerList, otherList)
	}
	meta := httptest.NewRequest(http.MethodGet, "/api/mobile/documents/shares/"+token, nil)
	meta.SetPathValue("token", token)
	metaRec := httptest.NewRecorder()
	CompanionDocumentShareHandler(identity).ServeHTTP(metaRec, meta)
	if metaRec.Code != http.StatusOK || strings.Contains(metaRec.Body.String(), "方法部分值得再看") || strings.Contains(metaRec.Body.String(), "note-reader@example.com") {
		t.Fatalf("public metadata leaked the note: %d %s", metaRec.Code, metaRec.Body.String())
	}
	stranger := postCompanionNote(t, identity, strangerToken, token, "我不该看见")
	if stranger.Code != http.StatusForbidden {
		t.Fatalf("stranger note: %d %s", stranger.Code, stranger.Body.String())
	}

	syncRec := syncCompanionShare(t, identity, ownerToken, token, []byte("%PDF-notes-v2"))
	if syncRec.Code != http.StatusOK {
		t.Fatalf("sync: %d %s", syncRec.Code, syncRec.Body.String())
	}
	if body := listCompanionShares(t, identity, otherToken); !strings.Contains(body, "方法部分值得再看") {
		t.Fatalf("sync dropped the note: %s", body)
	}

	remove := httptest.NewRequest(http.MethodDelete, "/api/mobile/documents/groups/"+groupID+"/members", strings.NewReader(`{"contact":"note-reader@example.com"}`))
	remove.SetPathValue("id", groupID)
	remove.Header.Set("Authorization", "Bearer "+ownerToken)
	removeRec := httptest.NewRecorder()
	CompanionDocumentGroupMembersHandler(identity).ServeHTTP(removeRec, remove)
	if removeRec.Code != http.StatusOK {
		t.Fatalf("remove: %d %s", removeRec.Code, removeRec.Body.String())
	}
	denied := postCompanionNote(t, identity, readerToken, token, "移出之后")
	if denied.Code != http.StatusForbidden || !strings.Contains(denied.Body.String(), "已不在群组") {
		t.Fatalf("removed note: %d %s", denied.Code, denied.Body.String())
	}
	if body := listCompanionShares(t, identity, otherToken); !strings.Contains(body, "方法部分值得再看") || strings.Contains(body, "移出之后") {
		t.Fatalf("remaining reader: %s", body)
	}
}

func TestCompanionDocumentShareHostReplyAnswersOneReaderNote(t *testing.T) {
	resetCompanionDocShares(t)
	identity, _, _ := newHTTPAPITestServices(t)
	ownerToken, _ := issueViewerToken(t, identity, "reply-owner@example.com")
	readerToken, _ := issueViewerToken(t, identity, "reply-reader@example.com")

	shareRec := postCompanionShareAs(t, identity, ownerToken, "person", "", "0", "reply-reader@example.com", "批注回复.pdf", []byte("%PDF-reply"))
	if shareRec.Code != http.StatusOK {
		t.Fatalf("share: %d %s", shareRec.Code, shareRec.Body.String())
	}
	var shared map[string]any
	if err := json.Unmarshal(shareRec.Body.Bytes(), &shared); err != nil {
		t.Fatal(err)
	}
	token, _ := shared["token"].(string)

	posted := postCompanionNote(t, identity, readerToken, token, "这个方法能复现吗？")
	if posted.Code != http.StatusOK {
		t.Fatalf("reader note: %d %s", posted.Code, posted.Body.String())
	}
	var readerList struct {
		Notes []struct {
			ID     string `json:"id"`
			Author string `json:"author"`
			Host   bool   `json:"host"`
			Text   string `json:"text"`
		} `json:"notes"`
		NotesRevision int `json:"notes_revision"`
	}
	if err := json.Unmarshal(posted.Body.Bytes(), &readerList); err != nil {
		t.Fatal(err)
	}
	if len(readerList.Notes) != 1 || readerList.Notes[0].Host {
		t.Fatalf("reader note was marked as the host's: %s", posted.Body.String())
	}
	noteID := readerList.Notes[0].ID

	reply := postCompanionNoteReply(t, identity, ownerToken, token, "可以，补充材料里有完整配置。", noteID)
	if reply.Code != http.StatusOK {
		t.Fatalf("host reply: %d %s", reply.Code, reply.Body.String())
	}
	var replyList struct {
		Notes []struct {
			ID      string `json:"id"`
			Author  string `json:"author"`
			Host    bool   `json:"host"`
			ReplyTo string `json:"reply_to"`
			Text    string `json:"text"`
		} `json:"notes"`
		NotesRevision int `json:"notes_revision"`
	}
	if err := json.Unmarshal(reply.Body.Bytes(), &replyList); err != nil {
		t.Fatal(err)
	}
	if replyList.NotesRevision != 2 || len(replyList.Notes) != 2 {
		t.Fatalf("host reply was not stored: %s", reply.Body.String())
	}
	answer := replyList.Notes[1]
	if !answer.Host || answer.ReplyTo != noteID || answer.Author != "reply-owner@example.com" || !strings.Contains(answer.Text, "补充材料") {
		t.Fatalf("host reply shape: %+v", answer)
	}

	// The answer is what every reader who can see the share reads, and the
	// list tells a desktop that this Hub knows how to link an answer.
	readerView := listCompanionShares(t, identity, readerToken)
	if !strings.Contains(readerView, `"notes_reply":true`) || !strings.Contains(readerView, `"server_time":`) || !strings.Contains(readerView, "补充材料里有完整配置") || !strings.Contains(readerView, `"reply_to":"`+noteID+`"`) {
		t.Fatalf("reader view: %s", readerView)
	}

	// A second host machine, or a retry after a lost response, must not add a
	// second answer to the same note.
	again := postCompanionNoteReply(t, identity, ownerToken, token, "重复回答", noteID)
	if again.Code != http.StatusOK {
		t.Fatalf("duplicate reply: %d %s", again.Code, again.Body.String())
	}
	var againList struct {
		NotesRevision int `json:"notes_revision"`
	}
	if err := json.Unmarshal(again.Body.Bytes(), &againList); err != nil {
		t.Fatal(err)
	}
	if againList.NotesRevision != replyList.NotesRevision {
		t.Fatalf("duplicate reply bumped the revision to %d", againList.NotesRevision)
	}

	// A reader's line is not an answer.
	readerReply := postCompanionNoteReply(t, identity, readerToken, token, "我也答一句", noteID)
	if readerReply.Code != http.StatusForbidden || !strings.Contains(readerReply.Body.String(), "只有主持人可以回复批注") {
		t.Fatalf("reader reply: %d %s", readerReply.Code, readerReply.Body.String())
	}
	missing := postCompanionNoteReply(t, identity, ownerToken, token, "回复不存在的批注", "cn_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")
	if missing.Code != http.StatusBadRequest || !strings.Contains(missing.Body.String(), "被回复的批注不存在") {
		t.Fatalf("missing target: %d %s", missing.Code, missing.Body.String())
	}
}

func TestCompanionShareNoteIsHostReadsLegacyAuthorID(t *testing.T) {
	share := companionDocShare{OwnerID: "owner-1"}
	legacy := companionShareNote{ID: "cn_1", AuthorID: "owner-1"}
	if !companionShareNoteIsHost(legacy, share.OwnerID) {
		t.Fatal("an owner note without the host flag was treated as a reader note")
	}
	if companionShareNoteIsHost(companionShareNote{ID: "cn_2", AuthorID: "reader-1"}, share.OwnerID) {
		t.Fatal("a reader note was treated as the host's")
	}
}

func TestCompanionShareHostReplyRetrySurvivesTrimmedQuestion(t *testing.T) {
	resetCompanionDocShares(t)
	identity, _, _ := newHTTPAPITestServices(t)
	ownerToken, _ := issueViewerToken(t, identity, "trim-owner@example.com")
	shareRec := postCompanionShareAs(t, identity, ownerToken, "public", "", "0", "", "a.pdf", []byte("%PDF-trim"))
	if shareRec.Code != http.StatusOK {
		t.Fatalf("share: %d %s", shareRec.Code, shareRec.Body.String())
	}
	var shared map[string]any
	if err := json.Unmarshal(shareRec.Body.Bytes(), &shared); err != nil {
		t.Fatal(err)
	}
	token, _ := shared["token"].(string)
	// The question has left the capped list; its answer has not.
	companionDocShares.Lock()
	share := companionDocShares.items[token]
	share.Notes = []companionShareNote{{
		ID: "cn_host", AuthorID: share.OwnerID, Author: "trim-owner@example.com",
		Text: "回答", Host: true, ReplyTo: "cn_gone", CreatedAt: time.Now().UTC(),
	}}
	share.NotesRevision = 2
	companionDocShares.items[token] = share
	companionDocShares.Unlock()
	retry := postCompanionNoteReply(t, identity, ownerToken, token, "重试同一回答", "cn_gone")
	if retry.Code != http.StatusOK || !strings.Contains(retry.Body.String(), `"notes_revision":2`) {
		t.Fatalf("retry: %d %s", retry.Code, retry.Body.String())
	}
	if strings.Contains(retry.Body.String(), "重试同一回答") {
		t.Fatalf("retry added a second note: %s", retry.Body.String())
	}
}

func TestCompanionDocumentGroupRenameIconLeaveAndDissolve(t *testing.T) {
	resetCompanionDocShares(t)
	identity, _, _ := newHTTPAPITestServices(t)
	ownerToken, _ := issueViewerToken(t, identity, "rename-owner@example.com")
	readerToken, _ := issueViewerToken(t, identity, "rename-reader@example.com")
	otherToken, _ := issueViewerToken(t, identity, "rename-other@example.com")
	pendingToken, _ := issueViewerToken(t, identity, "rename-pending@example.com")

	create := httptest.NewRequest(http.MethodPost, "/api/mobile/documents/groups", strings.NewReader(`{"name":"论文组"}`))
	create.Header.Set("Authorization", "Bearer "+ownerToken)
	createRec := httptest.NewRecorder()
	CompanionDocumentGroupsHandler(identity).ServeHTTP(createRec, create)
	if createRec.Code != http.StatusOK {
		t.Fatalf("create group: %d %s", createRec.Code, createRec.Body.String())
	}
	var created map[string]any
	if err := json.Unmarshal(createRec.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}
	groupID, _ := created["id"].(string)
	if !companionGroupTokenPattern.MatchString(groupID) || created["admin"] != true || created["icon"] != "book" {
		t.Fatalf("group: %s", createRec.Body.String())
	}

	for _, contact := range []string{"rename-reader@example.com", "rename-other@example.com", "rename-pending@example.com"} {
		add := httptest.NewRequest(http.MethodPost, "/api/mobile/documents/groups/"+groupID+"/members", strings.NewReader(`{"contact":"`+contact+`"}`))
		add.SetPathValue("id", groupID)
		add.Header.Set("Authorization", "Bearer "+ownerToken)
		addRec := httptest.NewRecorder()
		CompanionDocumentGroupMembersHandler(identity).ServeHTTP(addRec, add)
		if addRec.Code != http.StatusOK {
			t.Fatalf("add %s: %d %s", contact, addRec.Code, addRec.Body.String())
		}
	}
	for _, viewer := range []string{readerToken, otherToken} {
		accept := httptest.NewRequest(http.MethodPost, "/api/mobile/documents/groups/"+groupID+"/accept", nil)
		accept.SetPathValue("id", groupID)
		accept.Header.Set("Authorization", "Bearer "+viewer)
		acceptRec := httptest.NewRecorder()
		CompanionDocumentGroupAcceptHandler(identity).ServeHTTP(acceptRec, accept)
		if acceptRec.Code != http.StatusOK {
			t.Fatalf("accept: %d %s", acceptRec.Code, acceptRec.Body.String())
		}
	}

	shareRec := postCompanionShareAs(t, identity, ownerToken, "group", "", "0", groupID, "论文.pdf", []byte("%PDF-group"))
	if shareRec.Code != http.StatusOK {
		t.Fatalf("group share: %d %s", shareRec.Code, shareRec.Body.String())
	}
	var shared map[string]any
	if err := json.Unmarshal(shareRec.Body.Bytes(), &shared); err != nil {
		t.Fatal(err)
	}
	token, _ := shared["token"].(string)

	readerPatch := httptest.NewRequest(http.MethodPatch, "/api/mobile/documents/groups/"+groupID, strings.NewReader(`{"name":"被拒绝","icon":"star"}`))
	readerPatch.SetPathValue("id", groupID)
	readerPatch.Header.Set("Authorization", "Bearer "+readerToken)
	readerPatchRec := httptest.NewRecorder()
	CompanionDocumentGroupHandler(identity).ServeHTTP(readerPatchRec, readerPatch)
	if readerPatchRec.Code != http.StatusForbidden || !strings.Contains(readerPatchRec.Body.String(), "只有群组管理员可以修改群组") {
		t.Fatalf("reader patch: %d %s", readerPatchRec.Code, readerPatchRec.Body.String())
	}

	renamed := httptest.NewRequest(http.MethodPatch, "/api/mobile/documents/groups/"+groupID, strings.NewReader(`{"name":"新论文组","icon":"star"}`))
	renamed.SetPathValue("id", groupID)
	renamed.Header.Set("Authorization", "Bearer "+ownerToken)
	renamedRec := httptest.NewRecorder()
	CompanionDocumentGroupHandler(identity).ServeHTTP(renamedRec, renamed)
	if renamedRec.Code != http.StatusOK || !strings.Contains(renamedRec.Body.String(), "新论文组") || !strings.Contains(renamedRec.Body.String(), `"icon":"star"`) {
		t.Fatalf("rename: %d %s", renamedRec.Code, renamedRec.Body.String())
	}
	for _, viewer := range []string{readerToken, otherToken} {
		body := listCompanionGroups(t, identity, viewer)
		if !strings.Contains(body, groupID) || !strings.Contains(body, "新论文组") || !strings.Contains(body, `"icon":"star"`) {
			t.Fatalf("member group: %s", body)
		}
	}

	badIcon := httptest.NewRequest(http.MethodPatch, "/api/mobile/documents/groups/"+groupID, strings.NewReader(`{"name":"新论文组","icon":"rocket"}`))
	badIcon.SetPathValue("id", groupID)
	badIcon.Header.Set("Authorization", "Bearer "+ownerToken)
	badIconRec := httptest.NewRecorder()
	CompanionDocumentGroupHandler(identity).ServeHTTP(badIconRec, badIcon)
	if badIconRec.Code != http.StatusBadRequest || !strings.Contains(badIconRec.Body.String(), "请选择群组图标") {
		t.Fatalf("bad icon: %d %s", badIconRec.Code, badIconRec.Body.String())
	}
	if body := listCompanionGroups(t, identity, ownerToken); !strings.Contains(body, "新论文组") || strings.Contains(body, "rocket") {
		t.Fatalf("bad icon changed the group: %s", body)
	}
	longName := strings.Repeat("名", 41)
	tooLong := httptest.NewRequest(http.MethodPatch, "/api/mobile/documents/groups/"+groupID, strings.NewReader(`{"name":"`+longName+`","icon":"star"}`))
	tooLong.SetPathValue("id", groupID)
	tooLong.Header.Set("Authorization", "Bearer "+ownerToken)
	tooLongRec := httptest.NewRecorder()
	CompanionDocumentGroupHandler(identity).ServeHTTP(tooLongRec, tooLong)
	if tooLongRec.Code != http.StatusBadRequest || !strings.Contains(tooLongRec.Body.String(), "群组名称过长") {
		t.Fatalf("long name: %d %s", tooLongRec.Code, tooLongRec.Body.String())
	}

	readerDelete := httptest.NewRequest(http.MethodDelete, "/api/mobile/documents/groups/"+groupID, nil)
	readerDelete.SetPathValue("id", groupID)
	readerDelete.Header.Set("Authorization", "Bearer "+readerToken)
	readerDeleteRec := httptest.NewRecorder()
	CompanionDocumentGroupHandler(identity).ServeHTTP(readerDeleteRec, readerDelete)
	if readerDeleteRec.Code != http.StatusForbidden || !strings.Contains(readerDeleteRec.Body.String(), "只有群组管理员可以解散群组") {
		t.Fatalf("reader delete: %d %s", readerDeleteRec.Code, readerDeleteRec.Body.String())
	}

	ownerLeave := httptest.NewRequest(http.MethodPost, "/api/mobile/documents/groups/"+groupID+"/leave", strings.NewReader(`{}`))
	ownerLeave.SetPathValue("id", groupID)
	ownerLeave.Header.Set("Authorization", "Bearer "+ownerToken)
	ownerLeaveRec := httptest.NewRecorder()
	CompanionDocumentGroupLeaveHandler(identity).ServeHTTP(ownerLeaveRec, ownerLeave)
	if ownerLeaveRec.Code != http.StatusForbidden || !strings.Contains(ownerLeaveRec.Body.String(), "管理员请解散群组") {
		t.Fatalf("owner leave: %d %s", ownerLeaveRec.Code, ownerLeaveRec.Body.String())
	}

	pendingLeave := httptest.NewRequest(http.MethodPost, "/api/mobile/documents/groups/"+groupID+"/leave", strings.NewReader(`{}`))
	pendingLeave.SetPathValue("id", groupID)
	pendingLeave.Header.Set("Authorization", "Bearer "+pendingToken)
	pendingLeaveRec := httptest.NewRecorder()
	CompanionDocumentGroupLeaveHandler(identity).ServeHTTP(pendingLeaveRec, pendingLeave)
	if pendingLeaveRec.Code != http.StatusForbidden || !strings.Contains(pendingLeaveRec.Body.String(), "你不在这个群组") {
		t.Fatalf("pending leave: %d %s", pendingLeaveRec.Code, pendingLeaveRec.Body.String())
	}
	if invites := listCompanionInvites(t, identity, pendingToken); !strings.Contains(invites, groupID) {
		t.Fatalf("pending invite was dropped: %s", invites)
	}

	// The same person can accept once by email and once by another contact.
	// Leaving has to drop every accepted row, or the leftover row still sees the articles.
	readerUserID := ""
	loadCompanionDocGroups()
	companionDocGroups.Lock()
	stored := companionDocGroups.items[groupID]
	for _, member := range stored.Members {
		if member.Contact == "rename-reader@example.com" && member.Status == "accepted" {
			readerUserID = member.UserID
			break
		}
	}
	if readerUserID == "" {
		companionDocGroups.Unlock()
		t.Fatal("reader user id missing")
	}
	stored.Members = append(stored.Members, companionDocGroupMember{
		Contact: "13900001111",
		Status:  "accepted",
		UserID:  readerUserID,
	})
	companionDocGroups.items[groupID] = stored
	if err := persistCompanionDocGroupsLocked(); err != nil {
		companionDocGroups.Unlock()
		t.Fatal(err)
	}
	companionDocGroups.Unlock()

	readerLeave := httptest.NewRequest(http.MethodPost, "/api/mobile/documents/groups/"+groupID+"/leave", strings.NewReader(`{}`))
	readerLeave.SetPathValue("id", groupID)
	readerLeave.Header.Set("Authorization", "Bearer "+readerToken)
	readerLeaveRec := httptest.NewRecorder()
	CompanionDocumentGroupLeaveHandler(identity).ServeHTTP(readerLeaveRec, readerLeave)
	if readerLeaveRec.Code != http.StatusOK {
		t.Fatalf("reader leave: %d %s", readerLeaveRec.Code, readerLeaveRec.Body.String())
	}
	if body := listCompanionShares(t, identity, readerToken); strings.Contains(body, token) {
		t.Fatalf("reader still sees the group share: %s", body)
	}
	if body := listCompanionGroups(t, identity, readerToken); strings.Contains(body, groupID) {
		t.Fatalf("reader still in the group: %s", body)
	}
	if body := listCompanionShares(t, identity, otherToken); !strings.Contains(body, token) {
		t.Fatalf("other member lost the share: %s", body)
	}
	if body := listCompanionGroups(t, identity, ownerToken); strings.Contains(body, "rename-reader@example.com") || strings.Contains(body, "13900001111") || !strings.Contains(body, "rename-other@example.com") || !strings.Contains(body, "rename-pending@example.com") {
		t.Fatalf("leave kept a reader row or dropped someone else: %s", body)
	}

	otherDelete := httptest.NewRequest(http.MethodDelete, "/api/mobile/documents/groups/"+groupID, nil)
	otherDelete.SetPathValue("id", groupID)
	otherDelete.Header.Set("Authorization", "Bearer "+otherToken)
	otherDeleteRec := httptest.NewRecorder()
	CompanionDocumentGroupHandler(identity).ServeHTTP(otherDeleteRec, otherDelete)
	if otherDeleteRec.Code != http.StatusForbidden || !strings.Contains(otherDeleteRec.Body.String(), "只有群组管理员可以解散群组") {
		t.Fatalf("other delete: %d %s", otherDeleteRec.Code, otherDeleteRec.Body.String())
	}

	ownerDelete := httptest.NewRequest(http.MethodDelete, "/api/mobile/documents/groups/"+groupID, nil)
	ownerDelete.SetPathValue("id", groupID)
	ownerDelete.Header.Set("Authorization", "Bearer "+ownerToken)
	ownerDeleteRec := httptest.NewRecorder()
	CompanionDocumentGroupHandler(identity).ServeHTTP(ownerDeleteRec, ownerDelete)
	if ownerDeleteRec.Code != http.StatusOK {
		t.Fatalf("dissolve: %d %s", ownerDeleteRec.Code, ownerDeleteRec.Body.String())
	}
	if body := listCompanionGroups(t, identity, ownerToken); strings.Contains(body, groupID) {
		t.Fatalf("owner still lists the group: %s", body)
	}
	if body := listCompanionShares(t, identity, otherToken); strings.Contains(body, token) {
		t.Fatalf("member still sees the dissolved group share: %s", body)
	}
	if body := listCompanionGroups(t, identity, otherToken); strings.Contains(body, groupID) {
		t.Fatalf("member still lists the dissolved group: %s", body)
	}
	if body := listCompanionShares(t, identity, ownerToken); !strings.Contains(body, token) {
		t.Fatalf("dissolve removed the owner's share: %s", body)
	}

	again := postCompanionShareAs(t, identity, ownerToken, "group", "", "0", groupID, "再分享.pdf", []byte("%PDF-again"))
	if again.Code != http.StatusBadRequest || !strings.Contains(again.Body.String(), "请选择群组") {
		t.Fatalf("share after dissolve: %d %s", again.Code, again.Body.String())
	}

	missingID := "cg_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	missing := httptest.NewRequest(http.MethodDelete, "/api/mobile/documents/groups/"+missingID, nil)
	missing.SetPathValue("id", missingID)
	missing.Header.Set("Authorization", "Bearer "+ownerToken)
	missingRec := httptest.NewRecorder()
	CompanionDocumentGroupHandler(identity).ServeHTTP(missingRec, missing)
	if missingRec.Code != http.StatusNotFound || !strings.Contains(missingRec.Body.String(), "群组不存在") {
		t.Fatalf("missing group: %d %s", missingRec.Code, missingRec.Body.String())
	}
}

func postCompanionNote(t *testing.T, identity *auth.IdentityService, viewer, token, text string) *httptest.ResponseRecorder {
	t.Helper()
	return postCompanionNoteReply(t, identity, viewer, token, text, "")
}

func postCompanionNoteReply(t *testing.T, identity *auth.IdentityService, viewer, token, text, replyTo string) *httptest.ResponseRecorder {
	t.Helper()
	payload, err := json.Marshal(map[string]string{"text": text, "reply_to": replyTo})
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, "/api/mobile/documents/shares/"+token+"/notes", bytes.NewReader(payload))
	req.SetPathValue("token", token)
	req.Header.Set("Authorization", "Bearer "+viewer)
	rec := httptest.NewRecorder()
	CompanionDocumentShareNotesHandler(identity).ServeHTTP(rec, req)
	return rec
}

func postCompanionShareAs(t *testing.T, identity *auth.IdentityService, viewer, visibility, password, days, target, filename string, body []byte) *httptest.ResponseRecorder {
	t.Helper()
	var buf bytes.Buffer
	writer := multipart.NewWriter(&buf)
	_ = writer.WriteField("visibility", visibility)
	_ = writer.WriteField("password", password)
	_ = writer.WriteField("days", days)
	_ = writer.WriteField("target", target)
	_ = writer.WriteField("filename", filename)
	part, err := writer.CreateFormFile("file", "upload.bin")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := part.Write(body); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, "/api/mobile/documents/shares", &buf)
	req.Header.Set("Authorization", "Bearer "+viewer)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	rec := httptest.NewRecorder()
	CompanionDocumentSharesHandler(identity).ServeHTTP(rec, req)
	return rec
}

func syncCompanionShare(t *testing.T, identity *auth.IdentityService, viewer, token string, body []byte) *httptest.ResponseRecorder {
	t.Helper()
	var buf bytes.Buffer
	writer := multipart.NewWriter(&buf)
	part, err := writer.CreateFormFile("file", "upload.bin")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := part.Write(body); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, "/api/mobile/documents/shares/"+token+"/sync", &buf)
	req.SetPathValue("token", token)
	req.Header.Set("Authorization", "Bearer "+viewer)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	rec := httptest.NewRecorder()
	CompanionDocumentShareSyncHandler(identity).ServeHTTP(rec, req)
	return rec
}

func importCompanionShare(t *testing.T, identity *auth.IdentityService, viewer, token, password string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/api/mobile/documents/shares/"+token+"/import", strings.NewReader(`{"password":"`+password+`"}`))
	req.SetPathValue("token", token)
	req.Header.Set("Authorization", "Bearer "+viewer)
	rec := httptest.NewRecorder()
	CompanionDocumentShareImportHandler(identity).ServeHTTP(rec, req)
	return rec
}

func listCompanionShares(t *testing.T, identity *auth.IdentityService, viewer string) string {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/api/mobile/documents/shares", nil)
	req.Header.Set("Authorization", "Bearer "+viewer)
	rec := httptest.NewRecorder()
	CompanionDocumentSharesHandler(identity).ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("list shares: %d %s", rec.Code, rec.Body.String())
	}
	return rec.Body.String()
}

func listCompanionGroups(t *testing.T, identity *auth.IdentityService, viewer string) string {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/api/mobile/documents/groups", nil)
	req.Header.Set("Authorization", "Bearer "+viewer)
	rec := httptest.NewRecorder()
	CompanionDocumentGroupsHandler(identity).ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("list groups: %d %s", rec.Code, rec.Body.String())
	}
	return rec.Body.String()
}

func TestCompanionDocumentGroupInviteSettlesEveryPendingContact(t *testing.T) {
	resetCompanionDocShares(t)
	identity, _, _ := newHTTPAPITestServices(t)
	ownerToken, _ := issueViewerToken(t, identity, "multi-owner@example.com")
	readerToken, _ := issueViewerToken(t, identity, "multi-reader@example.com")
	principal, err := identity.AuthenticateViewer(context.Background(), readerToken)
	if err != nil || principal == nil || strings.TrimSpace(principal.UserID) == "" {
		t.Fatalf("reader principal: %+v %v", principal, err)
	}

	acceptID := inviteReaderOnTwoContacts(t, identity, ownerToken, principal.Email, principal.UserID)
	var listed struct {
		Invites []struct {
			ID        string `json:"id"`
			Contact   string `json:"contact"`
			InvitedAt string `json:"invited_at"`
		} `json:"invites"`
	}
	if err := json.Unmarshal([]byte(listCompanionInvites(t, identity, readerToken)), &listed); err != nil {
		t.Fatal(err)
	}
	if len(listed.Invites) != 1 || listed.Invites[0].ID != acceptID || listed.Invites[0].Contact != principal.UserID {
		t.Fatalf("newest invite: %+v", listed.Invites)
	}
	companionDocGroups.Lock()
	wantAt := ""
	for _, member := range companionDocGroups.items[acceptID].Members {
		if member.Contact == principal.UserID {
			wantAt = member.InvitedAt.UTC().Format(time.RFC3339)
		}
	}
	companionDocGroups.Unlock()
	if listed.Invites[0].InvitedAt != wantAt || wantAt == "" {
		t.Fatalf("invited_at %q want %q", listed.Invites[0].InvitedAt, wantAt)
	}
	postCompanionInviteDecision(t, identity, readerToken, acceptID, "accept")
	if after := listCompanionInvites(t, identity, readerToken); strings.Contains(after, acceptID) {
		t.Fatalf("accepted invite came back: %s", after)
	}
	if status, ok := companionMemberStatus(acceptID, strings.ToLower(principal.Email)); !ok || status != "accepted" {
		t.Fatalf("email row: %q %v", status, ok)
	}
	if status, ok := companionMemberStatus(acceptID, principal.UserID); !ok || status != "accepted" {
		t.Fatalf("second row: %q %v", status, ok)
	}

	declineID := inviteReaderOnTwoContacts(t, identity, ownerToken, principal.Email, principal.UserID)
	postCompanionInviteDecision(t, identity, readerToken, declineID, "decline")
	if after := listCompanionInvites(t, identity, readerToken); strings.Contains(after, declineID) {
		t.Fatalf("declined invite came back: %s", after)
	}
	if _, ok := companionMemberStatus(declineID, strings.ToLower(principal.Email)); ok {
		t.Fatal("declined email row remained")
	}
	if _, ok := companionMemberStatus(declineID, principal.UserID); ok {
		t.Fatal("declined second row remained")
	}
}

func inviteReaderOnTwoContacts(t *testing.T, identity *auth.IdentityService, ownerToken, email, other string) string {
	t.Helper()
	create := httptest.NewRequest(http.MethodPost, "/api/mobile/documents/groups", strings.NewReader(`{"name":"论文组"}`))
	create.Header.Set("Authorization", "Bearer "+ownerToken)
	createRec := httptest.NewRecorder()
	CompanionDocumentGroupsHandler(identity).ServeHTTP(createRec, create)
	if createRec.Code != http.StatusOK {
		t.Fatalf("create: %d %s", createRec.Code, createRec.Body.String())
	}
	var created map[string]any
	if err := json.Unmarshal(createRec.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}
	groupID, _ := created["id"].(string)
	add := httptest.NewRequest(http.MethodPost, "/api/mobile/documents/groups/"+groupID+"/members", strings.NewReader(`{"contact":"`+email+`"}`))
	add.SetPathValue("id", groupID)
	add.Header.Set("Authorization", "Bearer "+ownerToken)
	addRec := httptest.NewRecorder()
	CompanionDocumentGroupMembersHandler(identity).ServeHTTP(addRec, add)
	if addRec.Code != http.StatusOK {
		t.Fatalf("add: %d %s", addRec.Code, addRec.Body.String())
	}
	loadCompanionDocGroups()
	companionDocGroups.Lock()
	group := companionDocGroups.items[groupID]
	emailAt := time.Time{}
	for _, member := range group.Members {
		if strings.EqualFold(member.Contact, email) {
			emailAt = member.InvitedAt
		}
	}
	if emailAt.IsZero() {
		companionDocGroups.Unlock()
		t.Fatal("email invite missing")
	}
	group.Members = append(group.Members, companionDocGroupMember{
		Contact:   other,
		Status:    "pending",
		InvitedAt: emailAt.Add(time.Hour),
	})
	companionDocGroups.items[groupID] = group
	if err := persistCompanionDocGroupsLocked(); err != nil {
		companionDocGroups.Unlock()
		t.Fatal(err)
	}
	companionDocGroups.Unlock()
	return groupID
}

func postCompanionInviteDecision(t *testing.T, identity *auth.IdentityService, token, groupID, action string) {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/api/mobile/documents/groups/"+groupID+"/"+action, strings.NewReader(`{}`))
	req.SetPathValue("id", groupID)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	switch action {
	case "accept":
		CompanionDocumentGroupAcceptHandler(identity).ServeHTTP(rec, req)
	case "ignore":
		CompanionDocumentGroupIgnoreHandler(identity).ServeHTTP(rec, req)
	default:
		CompanionDocumentGroupDeclineHandler(identity).ServeHTTP(rec, req)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("%s: %d %s", action, rec.Code, rec.Body.String())
	}
}

func companionMemberStatus(groupID, contact string) (string, bool) {
	companionDocGroups.Lock()
	defer companionDocGroups.Unlock()
	group, ok := companionDocGroups.items[groupID]
	if !ok {
		return "", false
	}
	for _, member := range group.Members {
		if member.Contact == contact {
			return member.Status, true
		}
	}
	return "", false
}

func listCompanionInvites(t *testing.T, identity *auth.IdentityService, viewer string) string {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/api/mobile/documents/groups/invites", nil)
	req.Header.Set("Authorization", "Bearer "+viewer)
	rec := httptest.NewRecorder()
	CompanionDocumentGroupInvitesHandler(identity).ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("list invites: %d %s", rec.Code, rec.Body.String())
	}
	return rec.Body.String()
}
