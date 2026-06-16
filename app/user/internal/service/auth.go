package service

import (
	"context"
	pb "cwxu-algo/api/user/v1/auth"
	"cwxu-algo/app/common/conf"
	authutil "cwxu-algo/app/common/utils/auth"
	"cwxu-algo/app/user/internal/data"
	"cwxu-algo/app/user/internal/data/model"
	"encoding/json"
	"errors"
	"strings"
	"time"

	kerrors "github.com/go-kratos/kratos/v2/errors"
	"github.com/golang-jwt/jwt/v5"

	"gorm.io/gorm"
)

const (
	registerInviteCodeKey     = "register_invite_code"
	defaultRegisterInviteCode = "wustacm666"
)

type AuthService struct {
	db        *gorm.DB
	jwtSecret []byte
}

type UserOperationLogItem struct {
	ID           uint            `json:"id"`
	Service      string          `json:"service"`
	OperatorID   int64           `json:"operatorId"`
	OperatorRole int             `json:"operatorRole"`
	Action       string          `json:"action"`
	TargetType   string          `json:"targetType"`
	TargetID     int64           `json:"targetId"`
	Detail       json.RawMessage `json:"detail"`
	CreatedAt    int64           `json:"createdAt"`
}

type UserOperationLogRequest struct {
	Page     int64  `json:"page" form:"page"`
	PageSize int64  `json:"pageSize" form:"pageSize"`
	Action   string `json:"action" form:"action"`
}

type UserOperationLogReply struct {
	Code    int64                  `json:"code"`
	Message string                 `json:"message"`
	Data    []UserOperationLogItem `json:"data"`
	Total   int64                  `json:"total"`
}

func NewAuthService(d *data.Data, c *conf.Server) *AuthService {
	jwtSecret, err := authutil.JWTSecretBytes(c)
	if err != nil {
		panic(err)
	}
	return &AuthService{
		db:        d.DB,
		jwtSecret: jwtSecret,
	}
}

type RegisterInviteCodeRequest struct {
	InviteCode string `json:"inviteCode"`
}

type RegisterInviteCodeReply struct {
	Success    bool   `json:"success"`
	Message    string `json:"message"`
	InviteCode string `json:"inviteCode"`
}

// Login 登录
func (s *AuthService) Login(ctx context.Context, req *pb.LoginReq) (*pb.LoginRes, error) {
	res := &pb.LoginRes{}
	// 做校验
	u := &model.User{}
	r := s.db.Where("username = ?", req.Username).First(&u)
	if errors.Is(r.Error, gorm.ErrRecordNotFound) {
		res.Success = false
		res.Message = "用户名或密码错误"
		return res, nil
	}
	if r.Error != nil {
		res.Success = false
		res.Message = "登录失败"
		return res, nil
	}
	validPassword, needsRehash := verifyPassword(u.Password, req.Password)
	if !validPassword {
		res.Success = false
		res.Message = "用户名或密码错误"
		return res, nil
	}
	if needsRehash {
		if passwordHash, err := hashPassword(req.Password); err == nil {
			_ = s.db.Model(&model.User{}).Where("id = ?", u.ID).Update("password", passwordHash).Error
		}
	}
	// 签发 JWT Token
	expire := time.Now().Add(8640 * time.Hour) // 过期时间8640小时
	_roleIdsJSON := []byte("[0]")
	if u.RoleID == 1 || u.RoleID == 2 {
		_roleIdsJSON = []byte("[1]")
	}
	_jwtToken, err := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"userId":   u.ID,
		"username": u.Username,
		"name":     u.Name,
		"email":    u.Email,
		"roleId":   u.RoleID,
		"roleIds":  string(_roleIdsJSON),
		"exp":      expire.Unix(),
		"nbf":      time.Now().Unix(),
	}).SignedString(s.jwtSecret)
	if err != nil {
		res.Success = false
		res.Message = "身份校验成功，但是jwt生成失败了." + err.Error()
		return res, nil
	}
	res.Success = true
	res.Message = "登录成功"
	res.JwtToken = _jwtToken
	return res, nil
}

// Register 注册
func (s *AuthService) Register(ctx context.Context, req *pb.RegisterReq) (res *pb.RegisterRes, err error) {
	res = &pb.RegisterRes{
		Success: true,
		Message: "注册成功",
	}
	if ok, message := validateRegisterInviteCode(req.InviteCode, s.getRegisterInviteCode()); !ok {
		res.Success = false
		res.Message = message
		return
	}
	// 是否已经用户名
	users := make([]model.User, 0)
	s.db.Where("username = ?", req.Username).Find(&users)
	if len(users) >= 1 {
		res.Success = false
		res.Message = "用户名已经存在"
		return
	}
	// 尝试去注册
	passwordHash, hashErr := hashPassword(req.Password)
	if hashErr != nil {
		res.Success = false
		res.Message = "密码加密失败"
		return
	}
	newUser := &model.User{
		Username: req.Username,
		Password: passwordHash,
		Avatar:   "",
		Name:     req.Name,
		Email:    req.Email,
		GroupId:  0,
		RoleID:   0,
	}
	r := s.db.Create(&newUser)
	if r.Error != nil {
		res.Success = false
		res.Message = r.Error.Error()
	}
	return
}

func (s *AuthService) GetRegisterInviteCode(ctx context.Context) (*RegisterInviteCodeReply, error) {
	if !authutil.VerifyAdmin(ctx) {
		return nil, kerrors.Forbidden("权限不足", "只有管理员可以查看邀请码")
	}
	return &RegisterInviteCodeReply{
		Success:    true,
		Message:    "获取邀请码成功",
		InviteCode: s.getRegisterInviteCode(),
	}, nil
}

func (s *AuthService) UpdateRegisterInviteCode(ctx context.Context, req *RegisterInviteCodeRequest) (*RegisterInviteCodeReply, error) {
	if !authutil.VerifyAdmin(ctx) {
		return nil, kerrors.Forbidden("权限不足", "只有管理员可以修改邀请码")
	}
	inviteCode := strings.TrimSpace(req.InviteCode)
	if inviteCode == "" {
		return nil, kerrors.BadRequest("参数错误", "邀请码不能为空")
	}
	if err := s.saveRegisterInviteCode(inviteCode); err != nil {
		return nil, kerrors.InternalServer("保存失败", err.Error())
	}
	return &RegisterInviteCodeReply{
		Success:    true,
		Message:    "邀请码已更新",
		InviteCode: inviteCode,
	}, nil
}

func userOperationDetailJSON(detail string) json.RawMessage {
	if strings.TrimSpace(detail) == "" {
		return json.RawMessage("{}")
	}
	if json.Valid([]byte(detail)) {
		return json.RawMessage(detail)
	}
	encoded, _ := json.Marshal(map[string]string{"raw": detail})
	return encoded
}

func (s *AuthService) OperationLogs(ctx context.Context, req *UserOperationLogRequest) (*UserOperationLogReply, error) {
	if !authutil.VerifyAdmin(ctx) {
		return nil, kerrors.Forbidden("权限不足", "只有管理员可以查看操作日志")
	}
	page := req.Page
	pageSize := req.PageSize
	if page <= 0 {
		page = 1
	}
	if pageSize <= 0 || pageSize > 100 {
		pageSize = 30
	}
	query := s.db.Model(&model.OperationLog{})
	action := strings.TrimSpace(req.Action)
	if action != "" {
		query = query.Where("action = ?", action)
	}
	var total int64
	if err := query.Count(&total).Error; err != nil {
		return nil, kerrors.InternalServer("查询失败", err.Error())
	}
	var rows []model.OperationLog
	if err := query.Order("created_at DESC").Offset(int((page - 1) * pageSize)).Limit(int(pageSize)).Find(&rows).Error; err != nil {
		return nil, kerrors.InternalServer("查询失败", err.Error())
	}
	items := make([]UserOperationLogItem, 0, len(rows))
	for _, row := range rows {
		items = append(items, UserOperationLogItem{
			ID:           row.ID,
			Service:      "user",
			OperatorID:   row.OperatorID,
			OperatorRole: row.OperatorRole,
			Action:       row.Action,
			TargetType:   row.TargetType,
			TargetID:     row.TargetID,
			Detail:       userOperationDetailJSON(row.Detail),
			CreatedAt:    row.CreatedAt.Unix(),
		})
	}
	return &UserOperationLogReply{
		Code:    0,
		Message: "获取操作日志成功",
		Data:    items,
		Total:   total,
	}, nil
}

func (s *AuthService) getRegisterInviteCode() string {
	config := &model.SystemConfig{}
	result := s.db.Where("key = ?", registerInviteCodeKey).First(config)
	if errors.Is(result.Error, gorm.ErrRecordNotFound) {
		return defaultRegisterInviteCode
	}
	if result.Error != nil || strings.TrimSpace(config.Value) == "" {
		return defaultRegisterInviteCode
	}
	return strings.TrimSpace(config.Value)
}

func (s *AuthService) saveRegisterInviteCode(inviteCode string) error {
	config := &model.SystemConfig{}
	result := s.db.Where("key = ?", registerInviteCodeKey).First(config)
	if errors.Is(result.Error, gorm.ErrRecordNotFound) {
		return s.db.Create(&model.SystemConfig{
			Key:   registerInviteCodeKey,
			Value: inviteCode,
		}).Error
	}
	if result.Error != nil {
		return result.Error
	}
	config.Value = inviteCode
	return s.db.Save(config).Error
}
