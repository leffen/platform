// Copyright (c) 2017 Mattermost, Inc. All Rights Reserved.
// See License.txt for license information.

package api4

import (
	"bytes"
	"io"
	"net/http"
	"os"
	"reflect"
	"runtime/debug"
	"strconv"
	"strings"
	"testing"
	"time"

	l4g "github.com/alecthomas/log4go"
	"github.com/mattermost/platform/app"
	"github.com/mattermost/platform/model"
	"github.com/mattermost/platform/store"
	"github.com/mattermost/platform/utils"
	"github.com/mattermost/platform/wsapi"

	s3 "github.com/minio/minio-go"
)

type TestHelper struct {
	Client              *model.Client4
	BasicUser           *model.User
	BasicUser2          *model.User
	TeamAdminUser       *model.User
	BasicTeam           *model.Team
	BasicChannel        *model.Channel
	BasicPrivateChannel *model.Channel
	BasicChannel2       *model.Channel
	BasicPost           *model.Post

	SystemAdminClient *model.Client4
	SystemAdminUser   *model.User
}

func SetupEnterprise() *TestHelper {
	if app.Srv == nil {
		utils.TranslationsPreInit()
		utils.LoadConfig("config.json")
		utils.InitTranslations(utils.Cfg.LocalizationSettings)
		utils.Cfg.TeamSettings.MaxUsersPerTeam = 50
		*utils.Cfg.RateLimitSettings.Enable = false
		utils.Cfg.EmailSettings.SendEmailNotifications = true
		utils.Cfg.EmailSettings.SMTPServer = "dockerhost"
		utils.Cfg.EmailSettings.SMTPPort = "2500"
		utils.Cfg.EmailSettings.FeedbackEmail = "test@example.com"
		utils.DisableDebugLogForTest()
		utils.License.Features.SetDefaults()
		app.NewServer()
		app.InitStores()
		InitRouter()
		wsapi.InitRouter()
		app.StartServer()
		utils.InitHTML()
		InitApi(true)
		wsapi.InitApi()
		utils.EnableDebugLogForTest()
		app.Srv.Store.MarkSystemRanUnitTests()

		*utils.Cfg.TeamSettings.EnableOpenServer = true
	}

	th := &TestHelper{}
	th.Client = th.CreateClient()
	th.SystemAdminClient = th.CreateClient()
	return th
}

func Setup() *TestHelper {
	if app.Srv == nil {
		utils.TranslationsPreInit()
		utils.LoadConfig("config.json")
		utils.InitTranslations(utils.Cfg.LocalizationSettings)
		utils.Cfg.TeamSettings.MaxUsersPerTeam = 50
		*utils.Cfg.RateLimitSettings.Enable = false
		utils.Cfg.EmailSettings.SendEmailNotifications = true
		utils.Cfg.EmailSettings.SMTPServer = "dockerhost"
		utils.Cfg.EmailSettings.SMTPPort = "2500"
		utils.Cfg.EmailSettings.FeedbackEmail = "test@example.com"
		utils.DisableDebugLogForTest()
		app.NewServer()
		app.InitStores()
		InitRouter()
		wsapi.InitRouter()
		app.StartServer()
		InitApi(true)
		wsapi.InitApi()
		utils.EnableDebugLogForTest()
		app.Srv.Store.MarkSystemRanUnitTests()

		*utils.Cfg.TeamSettings.EnableOpenServer = true
	}

	th := &TestHelper{}
	th.Client = th.CreateClient()
	th.SystemAdminClient = th.CreateClient()
	return th
}

func TearDown() {
	utils.DisableDebugLogForTest()

	options := map[string]bool{}
	options[store.USER_SEARCH_OPTION_NAMES_ONLY_NO_FULL_NAME] = true
	if result := <-app.Srv.Store.User().Search("", "fakeuser", options); result.Err != nil {
		l4g.Error("Error tearing down test users")
	} else {
		users := result.Data.([]*model.User)

		for _, u := range users {
			if err := app.PermanentDeleteUser(u); err != nil {
				l4g.Error(err.Error())
			}
		}
	}

	if result := <-app.Srv.Store.Team().SearchByName("faketeam"); result.Err != nil {
		l4g.Error("Error tearing down test teams")
	} else {
		teams := result.Data.([]*model.Team)

		for _, t := range teams {
			if err := app.PermanentDeleteTeam(t); err != nil {
				l4g.Error(err.Error())
			}
		}
	}

	utils.EnableDebugLogForTest()
}

func (me *TestHelper) InitBasic() *TestHelper {
	me.TeamAdminUser = me.CreateUser()
	me.LoginTeamAdmin()
	me.BasicTeam = me.CreateTeam()
	me.BasicChannel = me.CreatePublicChannel()
	me.BasicPrivateChannel = me.CreatePrivateChannel()
	me.BasicChannel2 = me.CreatePublicChannel()
	me.BasicPost = me.CreatePost()
	me.BasicUser = me.CreateUser()
	LinkUserToTeam(me.BasicUser, me.BasicTeam)
	me.BasicUser2 = me.CreateUser()
	LinkUserToTeam(me.BasicUser2, me.BasicTeam)
	app.AddUserToChannel(me.BasicUser, me.BasicChannel)
	app.AddUserToChannel(me.BasicUser2, me.BasicChannel)
	app.AddUserToChannel(me.BasicUser, me.BasicChannel2)
	app.AddUserToChannel(me.BasicUser2, me.BasicChannel2)
	app.AddUserToChannel(me.BasicUser, me.BasicPrivateChannel)
	app.AddUserToChannel(me.BasicUser2, me.BasicPrivateChannel)
	app.UpdateUserRoles(me.BasicUser.Id, model.ROLE_SYSTEM_USER.Id)
	me.LoginBasic()

	return me
}

func (me *TestHelper) InitSystemAdmin() *TestHelper {
	me.SystemAdminUser = me.CreateUser()
	app.UpdateUserRoles(me.SystemAdminUser.Id, model.ROLE_SYSTEM_USER.Id+" "+model.ROLE_SYSTEM_ADMIN.Id)
	me.LoginSystemAdmin()

	return me
}

func (me *TestHelper) CreateClient() *model.Client4 {
	return model.NewAPIv4Client("http://localhost" + utils.Cfg.ServiceSettings.ListenAddress)
}

func (me *TestHelper) CreateWebSocketClient() (*model.WebSocketClient, *model.AppError) {
	return model.NewWebSocketClient4("ws://localhost"+utils.Cfg.ServiceSettings.ListenAddress, me.Client.AuthToken)
}

func (me *TestHelper) CreateUser() *model.User {
	return me.CreateUserWithClient(me.Client)
}

func (me *TestHelper) CreateTeam() *model.Team {
	return me.CreateTeamWithClient(me.Client)
}

func (me *TestHelper) CreateTeamWithClient(client *model.Client4) *model.Team {
	id := model.NewId()
	team := &model.Team{
		DisplayName: "dn_" + id,
		Name:        GenerateTestTeamName(),
		Email:       GenerateTestEmail(),
		Type:        model.TEAM_OPEN,
	}

	utils.DisableDebugLogForTest()
	rteam, _ := client.CreateTeam(team)
	utils.EnableDebugLogForTest()
	return rteam
}

func (me *TestHelper) CreateUserWithClient(client *model.Client4) *model.User {
	id := model.NewId()

	user := &model.User{
		Email:     GenerateTestEmail(),
		Username:  GenerateTestUsername(),
		Nickname:  "nn_" + id,
		FirstName: "f_" + id,
		LastName:  "l_" + id,
		Password:  "Password1",
	}

	utils.DisableDebugLogForTest()
	ruser, _ := client.CreateUser(user)
	ruser.Password = "Password1"
	VerifyUserEmail(ruser.Id)
	utils.EnableDebugLogForTest()
	return ruser
}

func (me *TestHelper) CreatePublicChannel() *model.Channel {
	return me.CreateChannelWithClient(me.Client, model.CHANNEL_OPEN)
}

func (me *TestHelper) CreatePrivateChannel() *model.Channel {
	return me.CreateChannelWithClient(me.Client, model.CHANNEL_PRIVATE)
}

func (me *TestHelper) CreateChannelWithClient(client *model.Client4, channelType string) *model.Channel {
	id := model.NewId()

	channel := &model.Channel{
		DisplayName: "dn_" + id,
		Name:        GenerateTestChannelName(),
		Type:        channelType,
		TeamId:      me.BasicTeam.Id,
	}

	utils.DisableDebugLogForTest()
	rchannel, _ := client.CreateChannel(channel)
	utils.EnableDebugLogForTest()
	return rchannel
}

func (me *TestHelper) CreatePost() *model.Post {
	return me.CreatePostWithClient(me.Client, me.BasicChannel)
}

func (me *TestHelper) CreatePinnedPost() *model.Post {
	return me.CreatePinnedPostWithClient(me.Client, me.BasicChannel)
}

func (me *TestHelper) CreateMessagePost(message string) *model.Post {
	return me.CreateMessagePostWithClient(me.Client, me.BasicChannel, message)
}

func (me *TestHelper) CreatePostWithClient(client *model.Client4, channel *model.Channel) *model.Post {
	id := model.NewId()

	post := &model.Post{
		ChannelId: channel.Id,
		Message:   "message_" + id,
	}

	utils.DisableDebugLogForTest()
	rpost, resp := client.CreatePost(post)
	if resp.Error != nil {
		panic(resp.Error)
	}
	utils.EnableDebugLogForTest()
	return rpost
}

func (me *TestHelper) CreatePinnedPostWithClient(client *model.Client4, channel *model.Channel) *model.Post {
	id := model.NewId()

	post := &model.Post{
		ChannelId: channel.Id,
		Message:   "message_" + id,
		IsPinned:  true,
	}

	utils.DisableDebugLogForTest()
	rpost, resp := client.CreatePost(post)
	if resp.Error != nil {
		panic(resp.Error)
	}
	utils.EnableDebugLogForTest()
	return rpost
}

func (me *TestHelper) CreateMessagePostWithClient(client *model.Client4, channel *model.Channel, message string) *model.Post {
	post := &model.Post{
		ChannelId: channel.Id,
		Message:   message,
	}

	utils.DisableDebugLogForTest()
	rpost, resp := client.CreatePost(post)
	if resp.Error != nil {
		panic(resp.Error)
	}
	utils.EnableDebugLogForTest()
	return rpost
}

func (me *TestHelper) LoginBasic() {
	me.LoginBasicWithClient(me.Client)
}

func (me *TestHelper) LoginBasic2() {
	me.LoginBasic2WithClient(me.Client)
}

func (me *TestHelper) LoginTeamAdmin() {
	me.LoginTeamAdminWithClient(me.Client)
}

func (me *TestHelper) LoginSystemAdmin() {
	me.LoginSystemAdminWithClient(me.SystemAdminClient)
}

func (me *TestHelper) LoginBasicWithClient(client *model.Client4) {
	utils.DisableDebugLogForTest()
	client.Login(me.BasicUser.Email, me.BasicUser.Password)
	utils.EnableDebugLogForTest()
}

func (me *TestHelper) LoginBasic2WithClient(client *model.Client4) {
	utils.DisableDebugLogForTest()
	client.Login(me.BasicUser2.Email, me.BasicUser2.Password)
	utils.EnableDebugLogForTest()
}

func (me *TestHelper) LoginTeamAdminWithClient(client *model.Client4) {
	utils.DisableDebugLogForTest()
	client.Login(me.TeamAdminUser.Email, me.TeamAdminUser.Password)
	utils.EnableDebugLogForTest()
}

func (me *TestHelper) LoginSystemAdminWithClient(client *model.Client4) {
	utils.DisableDebugLogForTest()
	client.Login(me.SystemAdminUser.Email, me.SystemAdminUser.Password)
	utils.EnableDebugLogForTest()
}

func (me *TestHelper) UpdateActiveUser(user *model.User, active bool) {
	utils.DisableDebugLogForTest()

	_, err := app.UpdateActive(user, active)
	if err != nil {
		l4g.Error(err.Error())
		l4g.Close()
		time.Sleep(time.Second)
		panic(err)
	}

	utils.EnableDebugLogForTest()
}

func LinkUserToTeam(user *model.User, team *model.Team) {
	utils.DisableDebugLogForTest()

	err := app.JoinUserToTeam(team, user, "")
	if err != nil {
		l4g.Error(err.Error())
		l4g.Close()
		time.Sleep(time.Second)
		panic(err)
	}

	utils.EnableDebugLogForTest()
}

func GenerateTestEmail() string {
	return strings.ToLower("success+" + model.NewId() + "@simulator.amazonses.com")
}

func GenerateTestUsername() string {
	return "fakeuser" + model.NewRandomString(13)
}

func GenerateTestTeamName() string {
	return "faketeam" + model.NewRandomString(6)
}

func GenerateTestChannelName() string {
	return "fakechannel" + model.NewRandomString(10)
}

func GenerateTestId() string {
	return model.NewId()
}

func VerifyUserEmail(userId string) {
	store.Must(app.Srv.Store.User().VerifyEmail(userId))
}

func CheckUserSanitization(t *testing.T, user *model.User) {
	if user.Password != "" {
		t.Fatal("password wasn't blank")
	}

	if user.AuthData != nil && *user.AuthData != "" {
		t.Fatal("auth data wasn't blank")
	}

	if user.MfaSecret != "" {
		t.Fatal("mfa secret wasn't blank")
	}
}

func CheckTeamSanitization(t *testing.T, team *model.Team) {
	if team.Email != "" {
		t.Fatal("email wasn't blank")
	}

	if team.AllowedDomains != "" {
		t.Fatal("'allowed domains' wasn't blank")
	}
}

func CheckEtag(t *testing.T, data interface{}, resp *model.Response) {
	if !reflect.ValueOf(data).IsNil() {
		debug.PrintStack()
		t.Fatal("etag data was not nil")
	}

	if resp.StatusCode != http.StatusNotModified {
		debug.PrintStack()
		t.Log("actual: " + strconv.Itoa(resp.StatusCode))
		t.Log("expected: " + strconv.Itoa(http.StatusNotModified))
		t.Fatal("wrong status code for etag")
	}
}

func CheckNoError(t *testing.T, resp *model.Response) {
	if resp.Error != nil {
		debug.PrintStack()
		t.Fatal("Expected no error, got " + resp.Error.Error())
	}
}

func CheckCreatedStatus(t *testing.T, resp *model.Response) {
	if resp.StatusCode != http.StatusCreated {
		debug.PrintStack()
		t.Log("actual: " + strconv.Itoa(resp.StatusCode))
		t.Log("expected: " + strconv.Itoa(http.StatusCreated))
		t.Fatal("wrong status code")
	}
}

func CheckForbiddenStatus(t *testing.T, resp *model.Response) {
	if resp.Error == nil {
		debug.PrintStack()
		t.Fatal("should have errored with status:" + strconv.Itoa(http.StatusForbidden))
		return
	}

	if resp.StatusCode != http.StatusForbidden {
		debug.PrintStack()
		t.Log("actual: " + strconv.Itoa(resp.StatusCode))
		t.Log("expected: " + strconv.Itoa(http.StatusForbidden))
		t.Fatal("wrong status code")
	}
}

func CheckUnauthorizedStatus(t *testing.T, resp *model.Response) {
	if resp.Error == nil {
		debug.PrintStack()
		t.Fatal("should have errored with status:" + strconv.Itoa(http.StatusUnauthorized))
		return
	}

	if resp.StatusCode != http.StatusUnauthorized {
		debug.PrintStack()
		t.Log("actual: " + strconv.Itoa(resp.StatusCode))
		t.Log("expected: " + strconv.Itoa(http.StatusUnauthorized))
		t.Fatal("wrong status code")
	}
}

func CheckNotFoundStatus(t *testing.T, resp *model.Response) {
	if resp.Error == nil {
		debug.PrintStack()
		t.Fatal("should have errored with status:" + strconv.Itoa(http.StatusNotFound))
		return
	}

	if resp.StatusCode != http.StatusNotFound {
		debug.PrintStack()
		t.Log("actual: " + strconv.Itoa(resp.StatusCode))
		t.Log("expected: " + strconv.Itoa(http.StatusNotFound))
		t.Fatal("wrong status code")
	}
}

func CheckBadRequestStatus(t *testing.T, resp *model.Response) {
	if resp.Error == nil {
		debug.PrintStack()
		t.Fatal("should have errored with status:" + strconv.Itoa(http.StatusBadRequest))
		return
	}

	if resp.StatusCode != http.StatusBadRequest {
		debug.PrintStack()
		t.Log("actual: " + strconv.Itoa(resp.StatusCode))
		t.Log("expected: " + strconv.Itoa(http.StatusBadRequest))
		t.Fatal("wrong status code")
	}
}

func CheckNotImplementedStatus(t *testing.T, resp *model.Response) {
	if resp.Error == nil {
		debug.PrintStack()
		t.Fatal("should have errored with status:" + strconv.Itoa(http.StatusNotImplemented))
		return
	}

	if resp.StatusCode != http.StatusNotImplemented {
		debug.PrintStack()
		t.Log("actual: " + strconv.Itoa(resp.StatusCode))
		t.Log("expected: " + strconv.Itoa(http.StatusNotImplemented))
		t.Fatal("wrong status code")
	}
}

func CheckOKStatus(t *testing.T, resp *model.Response) {
	CheckNoError(t, resp)

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("wrong status code. expected %d got %d", http.StatusOK, resp.StatusCode)
	}
}

func CheckErrorMessage(t *testing.T, resp *model.Response, errorId string) {
	if resp.Error == nil {
		debug.PrintStack()
		t.Fatal("should have errored with message:" + errorId)
		return
	}

	if resp.Error.Id != errorId {
		debug.PrintStack()
		t.Log("actual: " + resp.Error.Id)
		t.Log("expected: " + errorId)
		t.Fatal("incorrect error message")
	}
}

func CheckInternalErrorStatus(t *testing.T, resp *model.Response) {
	if resp.Error == nil {
		debug.PrintStack()
		t.Fatal("should have errored with status:" + strconv.Itoa(http.StatusInternalServerError))
		return
	}

	if resp.StatusCode != http.StatusInternalServerError {
		debug.PrintStack()
		t.Log("actual: " + strconv.Itoa(resp.StatusCode))
		t.Log("expected: " + strconv.Itoa(http.StatusInternalServerError))
		t.Fatal("wrong status code")
	}
}

func CheckPayLoadTooLargeStatus(t *testing.T, resp *model.Response) {
	if resp.Error == nil {
		debug.PrintStack()
		t.Fatal("should have errored with status:" + strconv.Itoa(http.StatusRequestEntityTooLarge))
		return
	}

	if resp.StatusCode != http.StatusRequestEntityTooLarge {
		debug.PrintStack()
		t.Log("actual: " + strconv.Itoa(resp.StatusCode))
		t.Log("expected: " + strconv.Itoa(http.StatusRequestEntityTooLarge))
		t.Fatal("wrong status code")
	}
}

func readTestFile(name string) ([]byte, error) {
	path := utils.FindDir("tests")
	file, err := os.Open(path + "/" + name)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	data := &bytes.Buffer{}
	if _, err := io.Copy(data, file); err != nil {
		return nil, err
	} else {
		return data.Bytes(), nil
	}
}

func cleanupTestFile(info *model.FileInfo) error {
	if utils.Cfg.FileSettings.DriverName == model.IMAGE_DRIVER_S3 {
		endpoint := utils.Cfg.FileSettings.AmazonS3Endpoint
		accessKey := utils.Cfg.FileSettings.AmazonS3AccessKeyId
		secretKey := utils.Cfg.FileSettings.AmazonS3SecretAccessKey
		secure := *utils.Cfg.FileSettings.AmazonS3SSL
		s3Clnt, err := s3.New(endpoint, accessKey, secretKey, secure)
		if err != nil {
			return err
		}
		bucket := utils.Cfg.FileSettings.AmazonS3Bucket
		if err := s3Clnt.RemoveObject(bucket, info.Path); err != nil {
			return err
		}

		if info.ThumbnailPath != "" {
			if err := s3Clnt.RemoveObject(bucket, info.ThumbnailPath); err != nil {
				return err
			}
		}

		if info.PreviewPath != "" {
			if err := s3Clnt.RemoveObject(bucket, info.PreviewPath); err != nil {
				return err
			}
		}
	} else if utils.Cfg.FileSettings.DriverName == model.IMAGE_DRIVER_LOCAL {
		if err := os.Remove(utils.Cfg.FileSettings.Directory + info.Path); err != nil {
			return err
		}

		if info.ThumbnailPath != "" {
			if err := os.Remove(utils.Cfg.FileSettings.Directory + info.ThumbnailPath); err != nil {
				return err
			}
		}

		if info.PreviewPath != "" {
			if err := os.Remove(utils.Cfg.FileSettings.Directory + info.PreviewPath); err != nil {
				return err
			}
		}
	}

	return nil
}

func MakeUserChannelAdmin(user *model.User, channel *model.Channel) {
	utils.DisableDebugLogForTest()

	if cmr := <-app.Srv.Store.Channel().GetMember(channel.Id, user.Id); cmr.Err == nil {
		cm := cmr.Data.(*model.ChannelMember)
		cm.Roles = "channel_admin channel_user"
		if sr := <-app.Srv.Store.Channel().UpdateMember(cm); sr.Err != nil {
			utils.EnableDebugLogForTest()
			panic(sr.Err)
		}
	} else {
		utils.EnableDebugLogForTest()
		panic(cmr.Err)
	}

	utils.EnableDebugLogForTest()
}

func UpdateUserToTeamAdmin(user *model.User, team *model.Team) {
	utils.DisableDebugLogForTest()

	tm := &model.TeamMember{TeamId: team.Id, UserId: user.Id, Roles: model.ROLE_TEAM_USER.Id + " " + model.ROLE_TEAM_ADMIN.Id}
	if tmr := <-app.Srv.Store.Team().UpdateMember(tm); tmr.Err != nil {
		utils.EnableDebugLogForTest()
		l4g.Error(tmr.Err.Error())
		l4g.Close()
		time.Sleep(time.Second)
		panic(tmr.Err)
	}
	utils.EnableDebugLogForTest()
}

func UpdateUserToNonTeamAdmin(user *model.User, team *model.Team) {
	utils.DisableDebugLogForTest()

	tm := &model.TeamMember{TeamId: team.Id, UserId: user.Id, Roles: model.ROLE_TEAM_USER.Id}
	if tmr := <-app.Srv.Store.Team().UpdateMember(tm); tmr.Err != nil {
		utils.EnableDebugLogForTest()
		l4g.Error(tmr.Err.Error())
		l4g.Close()
		time.Sleep(time.Second)
		panic(tmr.Err)
	}
	utils.EnableDebugLogForTest()
}
