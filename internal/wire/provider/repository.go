package provider

import (
	g "github.com/dinhdev-nu/chat-platform-api/global"
	"github.com/dinhdev-nu/chat-platform-api/internal/infrastructure/mysql/repo"
	r "github.com/dinhdev-nu/chat-platform-api/internal/repository"
)

func NewUserRepository() r.UserRepository {
	return repo.NewUserRepository(g.MySQLDB)
}

func NewUserTokenRepository() r.UserTokenRepository {
	return repo.NewUserTokenRepository(g.MySQLDB)
}

func NewRoomRepository() r.RoomRepository {
	return repo.NewRoomRepository(g.SqlDB)
}

func NewMessageRepository() r.MessageRepository {
	return repo.NewMessageRepository(g.SqlDB)
}
