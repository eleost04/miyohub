package mihoyo

const (
	TakumiAPI        = "https://api-takumi.mihoyo.com"
	BBSAPI           = "https://bbs-api.miyoushe.com"
	ZZZAPI           = "https://act-nap-api.mihoyo.com"
	AccountRolesPath = "/binding/api/getUserGameRolesByCookie"
	GameHomePath     = "/event/luna/home?lang=zh-cn"
	GameInfoPath     = "/event/luna/info?lang=zh-cn"
	GameSignPath     = "/event/luna/sign"
	ZZZHomePath      = "/event/luna/zzz/home?lang=zh-cn"
	ZZZInfoPath      = "/event/luna/zzz/info?lang=zh-cn"
	ZZZSignPath      = "/event/luna/zzz/sign"
	BBSSignPath      = "/apihub/app/api/signIn"
	BBSStatePath     = "/apihub/wapi/getUserMissionsState"
	BBSUpvotePath    = "/apihub/sapi/upvotePost"
	BBSPostVotePath  = "/post/api/post/upvote"
	MallGoodsPath    = "/mall/v1/web/goods/list"
	MallDetailPath   = "/mall/v1/web/goods/detail"
	MallExchangePath = "https://api-takumi.miyoushe.com/mall/v1/web/goods/exchange"
	MallPointPath    = "/common/homutreasure/v1/web/user/point"
	MallAddressPath  = "/account/address/list"
	DeviceFPURL      = "https://public-data-api.mihoyo.com/device-fp/api/getFp"
)

type Game struct {
	Key      string
	Name     string
	GameBiz  string
	ActID    string
	HomeURL  string
	InfoURL  string
	SignURL  string
	SignGame string
}

var Games = map[string]Game{
	"genshin":   {Key: "genshin", Name: "原神", GameBiz: "hk4e_cn", ActID: "e202311201442471", HomeURL: TakumiAPI + GameHomePath, InfoURL: TakumiAPI + GameInfoPath, SignURL: TakumiAPI + GameSignPath, SignGame: "hk4e"},
	"starrail":  {Key: "starrail", Name: "崩坏：星穹铁道", GameBiz: "hkrpg_cn", ActID: "e202304121516551", HomeURL: TakumiAPI + GameHomePath, InfoURL: TakumiAPI + GameInfoPath, SignURL: TakumiAPI + GameSignPath},
	"zzz":       {Key: "zzz", Name: "绝区零", GameBiz: "nap_cn", ActID: "e202406242138391", HomeURL: ZZZAPI + ZZZHomePath, InfoURL: ZZZAPI + ZZZInfoPath, SignURL: ZZZAPI + ZZZSignPath, SignGame: "zzz"},
	"honkai3rd": {Key: "honkai3rd", Name: "崩坏3", GameBiz: "bh3_cn", ActID: "e202306201626331", HomeURL: TakumiAPI + GameHomePath, InfoURL: TakumiAPI + GameInfoPath, SignURL: TakumiAPI + GameSignPath},
	"tears":     {Key: "tears", Name: "未定事件簿", GameBiz: "nxx_cn", ActID: "e202202251749321", HomeURL: TakumiAPI + GameHomePath, InfoURL: TakumiAPI + GameInfoPath, SignURL: TakumiAPI + GameSignPath},
	"honkai2":   {Key: "honkai2", Name: "崩坏学园2", GameBiz: "bh2_cn", ActID: "e202203291431091", HomeURL: TakumiAPI + GameHomePath, InfoURL: TakumiAPI + GameInfoPath, SignURL: TakumiAPI + GameSignPath},
}

var BBSForums = map[int]struct{ ID, ForumID, Name string }{
	1: {"1", "1", "崩坏3"},
	2: {"2", "26", "原神"},
	3: {"3", "30", "崩坏学园2"},
	4: {"4", "37", "未定事件簿"},
	5: {"5", "34", "大别野"},
	6: {"6", "52", "崩坏：星穹铁道"},
	8: {"8", "57", "绝区零"},
}
