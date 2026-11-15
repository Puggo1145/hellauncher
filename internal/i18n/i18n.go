package i18n

import (
	"errors"
	"fmt"
	"path"
	"strings"

	"hellauncher/internal/catalog"
)

// IconMeta pairs an embedded official Helldivers 2 Stratagem SVG key with its
// localized names, canonical category color, and authentic arrow sequence.
type IconMeta struct {
	Key      string
	NameZH   string
	NameEN   string
	Category catalog.Category
	Code     []catalog.Dir
}

func (m IconMeta) Name(lang catalog.Language) string {
	if lang == catalog.LangEN {
		return m.NameEN
	}
	return m.NameZH
}

// Icons lists all 111 official Helldivers 2 Stratagem presets in canonical display order.
var Icons = []IconMeta{
	// Offensive (Red)
	{Key: "eagle-500kg.svg", NameZH: "“飞鹰”500KG炸弹", NameEN: "Eagle 500kg Bomb", Category: catalog.CategoryRed, Code: []catalog.Dir{catalog.DirUp, catalog.DirRight, catalog.DirDown, catalog.DirDown, catalog.DirDown}},
	{Key: "eagle-airstrike.svg", NameZH: "“飞鹰”空袭", NameEN: "Eagle Airstrike", Category: catalog.CategoryRed, Code: []catalog.Dir{catalog.DirUp, catalog.DirRight, catalog.DirDown, catalog.DirRight}},
	{Key: "eagle-strafing-run.svg", NameZH: "“飞鹰”机枪扫射", NameEN: "Eagle Strafing Run", Category: catalog.CategoryRed, Code: []catalog.Dir{catalog.DirUp, catalog.DirRight, catalog.DirRight}},
	{Key: "eagle-cluster-bomb.svg", NameZH: "“飞鹰”集束炸弹", NameEN: "Eagle Cluster Bomb", Category: catalog.CategoryRed, Code: []catalog.Dir{catalog.DirUp, catalog.DirRight, catalog.DirDown, catalog.DirDown, catalog.DirRight}},
	{Key: "eagle-napalm-airstrike.svg", NameZH: "“飞鹰”凝固汽油弹空袭", NameEN: "Eagle Napalm Airstrike", Category: catalog.CategoryRed, Code: []catalog.Dir{catalog.DirUp, catalog.DirRight, catalog.DirDown, catalog.DirUp}},
	{Key: "eagle-110mm-rocket-pods.svg", NameZH: "“飞鹰”110MM火箭巢", NameEN: "Eagle 110mm Rocket Pods", Category: catalog.CategoryRed, Code: []catalog.Dir{catalog.DirUp, catalog.DirRight, catalog.DirUp, catalog.DirLeft}},
	{Key: "eagle-smoke-strike.svg", NameZH: "“飞鹰”烟雾攻击", NameEN: "Eagle Smoke Strike", Category: catalog.CategoryRed, Code: []catalog.Dir{catalog.DirUp, catalog.DirRight, catalog.DirUp, catalog.DirDown}},
	{Key: "eagle-gas-strike.svg", NameZH: "“飞鹰”毒气攻击", NameEN: "Eagle Gas Strike", Category: catalog.CategoryRed, Code: []catalog.Dir{catalog.DirUp, catalog.DirRight, catalog.DirLeft, catalog.DirRight}},
	{Key: "orbital-precision.svg", NameZH: "轨道精准攻击", NameEN: "Orbital Precision Strike", Category: catalog.CategoryRed, Code: []catalog.Dir{catalog.DirRight, catalog.DirRight, catalog.DirUp}},
	{Key: "orbital-gatling-barrage.svg", NameZH: "轨道加特林火力网", NameEN: "Orbital Gatling Barrage", Category: catalog.CategoryRed, Code: []catalog.Dir{catalog.DirRight, catalog.DirDown, catalog.DirLeft, catalog.DirUp, catalog.DirUp}},
	{Key: "orbital-airburst-strike.svg", NameZH: "轨道空爆攻击", NameEN: "Orbital Airburst Strike", Category: catalog.CategoryRed, Code: []catalog.Dir{catalog.DirRight, catalog.DirRight, catalog.DirRight}},
	{Key: "orbital-120mm-he-barrage.svg", NameZH: "轨道120MM高爆弹火力网", NameEN: "Orbital 120mm HE Barrage", Category: catalog.CategoryRed, Code: []catalog.Dir{catalog.DirRight, catalog.DirRight, catalog.DirDown, catalog.DirLeft, catalog.DirRight, catalog.DirDown}},
	{Key: "orbital-380mm-he-barrage.svg", NameZH: "轨道380MM高爆弹火力网", NameEN: "Orbital 380mm HE Barrage", Category: catalog.CategoryRed, Code: []catalog.Dir{catalog.DirRight, catalog.DirDown, catalog.DirUp, catalog.DirUp, catalog.DirLeft, catalog.DirDown, catalog.DirDown}},
	{Key: "orbital-walking-barrage.svg", NameZH: "轨道游走火力网", NameEN: "Orbital Walking Barrage", Category: catalog.CategoryRed, Code: []catalog.Dir{catalog.DirRight, catalog.DirDown, catalog.DirRight, catalog.DirDown, catalog.DirRight, catalog.DirDown}},
	{Key: "orbital-laser.svg", NameZH: "轨道激光炮", NameEN: "Orbital Laser", Category: catalog.CategoryRed, Code: []catalog.Dir{catalog.DirRight, catalog.DirDown, catalog.DirUp, catalog.DirRight, catalog.DirDown}},
	{Key: "orbital-napalm-barrage.svg", NameZH: "轨道凝固汽油弹火力网", NameEN: "Orbital Napalm Barrage", Category: catalog.CategoryRed, Code: []catalog.Dir{catalog.DirRight, catalog.DirRight, catalog.DirDown, catalog.DirLeft, catalog.DirRight, catalog.DirUp}},
	{Key: "orbital-railcannon.svg", NameZH: "轨道电磁炮攻击", NameEN: "Orbital Railcannon Strike", Category: catalog.CategoryRed, Code: []catalog.Dir{catalog.DirRight, catalog.DirUp, catalog.DirDown, catalog.DirDown, catalog.DirRight}},
	{Key: "orbital-gas-strike.svg", NameZH: "轨道毒气攻击", NameEN: "Orbital Gas Strike", Category: catalog.CategoryRed, Code: []catalog.Dir{catalog.DirRight, catalog.DirRight, catalog.DirDown, catalog.DirRight}},
	{Key: "orbital-ems-strike.svg", NameZH: "轨道电磁冲击波攻击", NameEN: "Orbital EMS Strike", Category: catalog.CategoryRed, Code: []catalog.Dir{catalog.DirRight, catalog.DirRight, catalog.DirLeft, catalog.DirDown}},
	{Key: "orbital-smoke-strike.svg", NameZH: "轨道烟雾攻击", NameEN: "Orbital Smoke Strike", Category: catalog.CategoryRed, Code: []catalog.Dir{catalog.DirRight, catalog.DirRight, catalog.DirDown, catalog.DirUp}},

	// Supply, Support Weapons, Backpacks & Vehicles (Blue)
	{Key: "machine-gun.svg", NameZH: "MG-43 机枪", NameEN: "MG-43 Machine Gun", Category: catalog.CategoryBlue, Code: []catalog.Dir{catalog.DirDown, catalog.DirLeft, catalog.DirDown, catalog.DirUp, catalog.DirRight}},
	{Key: "anti-materiel-rifle.svg", NameZH: "APW-1 反器材步枪", NameEN: "APW-1 Anti-Materiel Rifle", Category: catalog.CategoryBlue, Code: []catalog.Dir{catalog.DirDown, catalog.DirLeft, catalog.DirRight, catalog.DirUp, catalog.DirDown}},
	{Key: "stalwart.svg", NameZH: "M-105 盟友", NameEN: "M-105 Stalwart", Category: catalog.CategoryBlue, Code: []catalog.Dir{catalog.DirDown, catalog.DirLeft, catalog.DirDown, catalog.DirUp, catalog.DirUp, catalog.DirLeft}},
	{Key: "expendable-anti-tank.svg", NameZH: "EAT-17 消耗性反坦克武器", NameEN: "EAT-17 Expendable Anti-Tank", Category: catalog.CategoryBlue, Code: []catalog.Dir{catalog.DirDown, catalog.DirDown, catalog.DirLeft, catalog.DirUp, catalog.DirRight}},
	{Key: "recoilless-rifle.svg", NameZH: "GR-8 无后坐力步枪", NameEN: "GR-8 Recoilless Rifle", Category: catalog.CategoryBlue, Code: []catalog.Dir{catalog.DirDown, catalog.DirLeft, catalog.DirRight, catalog.DirRight, catalog.DirLeft}},
	{Key: "flamethrower.svg", NameZH: "FLAM-40 火焰喷射器", NameEN: "FLAM-40 Flamethrower", Category: catalog.CategoryBlue, Code: []catalog.Dir{catalog.DirDown, catalog.DirLeft, catalog.DirUp, catalog.DirDown, catalog.DirUp}},
	{Key: "autocannon.svg", NameZH: "AC-8 机炮", NameEN: "AC-8 Autocannon", Category: catalog.CategoryBlue, Code: []catalog.Dir{catalog.DirDown, catalog.DirLeft, catalog.DirDown, catalog.DirUp, catalog.DirUp, catalog.DirRight}},
	{Key: "heavy-machine-gun.svg", NameZH: "MG-206 重机枪", NameEN: "MG-206 Heavy Machine Gun", Category: catalog.CategoryBlue, Code: []catalog.Dir{catalog.DirDown, catalog.DirLeft, catalog.DirUp, catalog.DirDown, catalog.DirDown}},
	{Key: "airburst-rocket-launcher.svg", NameZH: "RL-77 空爆火箭弹发射器", NameEN: "RL-77 Airburst Rocket Launcher", Category: catalog.CategoryBlue, Code: []catalog.Dir{catalog.DirDown, catalog.DirUp, catalog.DirUp, catalog.DirLeft, catalog.DirRight}},
	{Key: "commando.svg", NameZH: "MLS-4X 突击兵", NameEN: "MLS-4X Commando", Category: catalog.CategoryBlue, Code: []catalog.Dir{catalog.DirDown, catalog.DirLeft, catalog.DirUp, catalog.DirDown, catalog.DirRight}},
	{Key: "railgun.svg", NameZH: "RS-422 磁轨炮", NameEN: "RS-422 Railgun", Category: catalog.CategoryBlue, Code: []catalog.Dir{catalog.DirDown, catalog.DirRight, catalog.DirDown, catalog.DirUp, catalog.DirLeft, catalog.DirRight}},
	{Key: "spear.svg", NameZH: "FAF-14 飞矛", NameEN: "FAF-14 Spear", Category: catalog.CategoryBlue, Code: []catalog.Dir{catalog.DirDown, catalog.DirDown, catalog.DirUp, catalog.DirDown, catalog.DirDown}},
	{Key: "sta-x3-wasp-launcher.svg", NameZH: "StA-X3 W.A.S.P. 发射器", NameEN: "StA-X3 W.A.S.P. Launcher", Category: catalog.CategoryBlue, Code: []catalog.Dir{catalog.DirDown, catalog.DirDown, catalog.DirUp, catalog.DirDown, catalog.DirRight}},
	{Key: "grenade-launcher.svg", NameZH: "GL-21 榴弹发射器", NameEN: "GL-21 Grenade Launcher", Category: catalog.CategoryBlue, Code: []catalog.Dir{catalog.DirDown, catalog.DirLeft, catalog.DirUp, catalog.DirLeft, catalog.DirDown}},
	{Key: "laser-cannon.svg", NameZH: "LAS-98 激光大炮", NameEN: "LAS-98 Laser Cannon", Category: catalog.CategoryBlue, Code: []catalog.Dir{catalog.DirDown, catalog.DirLeft, catalog.DirDown, catalog.DirUp, catalog.DirLeft}},
	{Key: "arc-thrower.svg", NameZH: "ARC-3 电弧发射器", NameEN: "ARC-3 Arc Thrower", Category: catalog.CategoryBlue, Code: []catalog.Dir{catalog.DirDown, catalog.DirRight, catalog.DirDown, catalog.DirUp, catalog.DirLeft, catalog.DirLeft}},
	{Key: "quasar-cannon.svg", NameZH: "LAS-99 类星体加农炮", NameEN: "LAS-99 Quasar Cannon", Category: catalog.CategoryBlue, Code: []catalog.Dir{catalog.DirDown, catalog.DirDown, catalog.DirUp, catalog.DirLeft, catalog.DirRight}},
	{Key: "sterilizer.svg", NameZH: "TX-41 灭菌器", NameEN: "TX-41 Sterilizer", Category: catalog.CategoryBlue, Code: []catalog.Dir{catalog.DirDown, catalog.DirLeft, catalog.DirUp, catalog.DirDown, catalog.DirLeft}},
	{Key: "one-true-flag.svg", NameZH: "CQC-1 唯一真旗", NameEN: "CQC-1 One True Flag", Category: catalog.CategoryBlue, Code: []catalog.Dir{catalog.DirDown, catalog.DirLeft, catalog.DirRight, catalog.DirRight, catalog.DirUp}},
	{Key: "gl-52-de-escalator.svg", NameZH: "GL-52 缓和使者", NameEN: "GL-52 De-Escalator", Category: catalog.CategoryBlue, Code: []catalog.Dir{catalog.DirDown, catalog.DirRight, catalog.DirUp, catalog.DirLeft, catalog.DirRight}},
	{Key: "epoch.svg", NameZH: "PLAS-45 纪元等离子炮", NameEN: "PLAS-45 Epoch", Category: catalog.CategoryBlue, Code: []catalog.Dir{catalog.DirDown, catalog.DirLeft, catalog.DirUp, catalog.DirLeft, catalog.DirRight}},
	{Key: "speargun.svg", NameZH: "S-11 鱼叉枪", NameEN: "S-11 Speargun", Category: catalog.CategoryBlue, Code: []catalog.Dir{catalog.DirDown, catalog.DirRight, catalog.DirDown, catalog.DirLeft, catalog.DirUp, catalog.DirRight}},
	{Key: "expendable-napalm.svg", NameZH: "EAT-700 消耗性凝固汽油弹", NameEN: "EAT-700 Expendable Napalm", Category: catalog.CategoryBlue, Code: []catalog.Dir{catalog.DirDown, catalog.DirDown, catalog.DirLeft, catalog.DirUp, catalog.DirLeft}},
	{Key: "solo-silo.svg", NameZH: "MS-11 单兵导弹发射井", NameEN: "MS-11 Solo Silo", Category: catalog.CategoryBlue, Code: []catalog.Dir{catalog.DirDown, catalog.DirUp, catalog.DirRight, catalog.DirDown, catalog.DirDown}},
	{Key: "maxigun.svg", NameZH: "M-1000 马克西转管机枪", NameEN: "M-1000 Maxigun", Category: catalog.CategoryBlue, Code: []catalog.Dir{catalog.DirDown, catalog.DirLeft, catalog.DirRight, catalog.DirDown, catalog.DirUp, catalog.DirUp}},
	{Key: "defoliation-tool.svg", NameZH: "CQC-9 落叶链锯", NameEN: "CQC-9 Defoliation Tool", Category: catalog.CategoryBlue, Code: []catalog.Dir{catalog.DirDown, catalog.DirLeft, catalog.DirRight, catalog.DirRight, catalog.DirDown}},
	{Key: "cqc-20.svg", NameZH: "CQC-20 破门战锤", NameEN: "CQC-20 Breaching Hammer", Category: catalog.CategoryBlue, Code: []catalog.Dir{catalog.DirDown, catalog.DirLeft, catalog.DirRight, catalog.DirLeft, catalog.DirUp}},
	{Key: "eat-411.svg", NameZH: "EAT-411 夷平者", NameEN: "EAT-411 Leveller", Category: catalog.CategoryBlue, Code: []catalog.Dir{catalog.DirDown, catalog.DirDown, catalog.DirLeft, catalog.DirUp, catalog.DirDown}},
	{Key: "gl-28.svg", NameZH: "GL-28 弹链榴弹发射器", NameEN: "GL-28 Belt-Fed Grenade Launcher", Category: catalog.CategoryBlue, Code: []catalog.Dir{catalog.DirDown, catalog.DirLeft, catalog.DirUp, catalog.DirLeft, catalog.DirUp, catalog.DirUp}},
	{Key: "cremator.svg", NameZH: "B/FLAM-80 焚化者", NameEN: "B/FLAM-80 Cremator", Category: catalog.CategoryBlue, Code: []catalog.Dir{catalog.DirDown, catalog.DirDown, catalog.DirRight, catalog.DirDown, catalog.DirUp, catalog.DirUp}},
	{Key: "bullet-storm.svg", NameZH: "弹幕风暴背包", NameEN: "Bullet Storm", Category: catalog.CategoryBlue, Code: []catalog.Dir{catalog.DirDown, catalog.DirLeft, catalog.DirDown, catalog.DirRight, catalog.DirUp, catalog.DirLeft}},
	{Key: "40-k-meltagun.svg", NameZH: "40-K 热熔枪", NameEN: "40-K Meltagun", Category: catalog.CategoryBlue, Code: []catalog.Dir{catalog.DirDown, catalog.DirLeft, catalog.DirUp, catalog.DirLeft, catalog.DirLeft, catalog.DirDown}},
	{Key: "las-44-nova.svg", NameZH: "LAS-44 新星激光狙击枪", NameEN: "LAS-44 Nova", Category: catalog.CategoryBlue, Code: []catalog.Dir{catalog.DirDown, catalog.DirLeft, catalog.DirRight, catalog.DirDown, catalog.DirDown}},
	{Key: "amw-20-apex.svg", NameZH: "AMW-20 巅峰反坦克步枪", NameEN: "AMW-20 Apex", Category: catalog.CategoryBlue, Code: []catalog.Dir{catalog.DirDown, catalog.DirLeft, catalog.DirLeft, catalog.DirUp, catalog.DirDown, catalog.DirLeft}},
	{Key: "jump-pack.svg", NameZH: "LIFT-850 喷射背包", NameEN: "LIFT-850 Jump Pack", Category: catalog.CategoryBlue, Code: []catalog.Dir{catalog.DirDown, catalog.DirUp, catalog.DirUp, catalog.DirDown, catalog.DirUp}},
	{Key: "hover-pack.svg", NameZH: "LIFT-860 悬停背包", NameEN: "LIFT-860 Hover Pack", Category: catalog.CategoryBlue, Code: []catalog.Dir{catalog.DirDown, catalog.DirUp, catalog.DirUp, catalog.DirDown, catalog.DirLeft, catalog.DirRight}},
	{Key: "warp-pack.svg", NameZH: "LIFT-182 跃迁背包", NameEN: "LIFT-182 Warp Pack", Category: catalog.CategoryBlue, Code: []catalog.Dir{catalog.DirDown, catalog.DirLeft, catalog.DirRight, catalog.DirDown, catalog.DirLeft, catalog.DirRight}},
	{Key: "supply-pack.svg", NameZH: "B-1 补给背包", NameEN: "B-1 Supply Pack", Category: catalog.CategoryBlue, Code: []catalog.Dir{catalog.DirDown, catalog.DirLeft, catalog.DirDown, catalog.DirUp, catalog.DirUp, catalog.DirDown}},
	{Key: "hellbomb-portable.svg", NameZH: "B-100 便携式地狱火炸弹", NameEN: "B-100 Portable Hellbomb", Category: catalog.CategoryBlue, Code: []catalog.Dir{catalog.DirDown, catalog.DirRight, catalog.DirUp, catalog.DirUp, catalog.DirUp}},
	{Key: "c4-pack.svg", NameZH: "B/MD C4 炸药包", NameEN: "B/MD C4 Pack", Category: catalog.CategoryBlue, Code: []catalog.Dir{catalog.DirDown, catalog.DirRight, catalog.DirUp, catalog.DirUp, catalog.DirRight, catalog.DirUp}},
	{Key: "ballistic-shield-backpack.svg", NameZH: "SH-20 防弹护盾背包", NameEN: "SH-20 Ballistic Shield Backpack", Category: catalog.CategoryBlue, Code: []catalog.Dir{catalog.DirDown, catalog.DirLeft, catalog.DirDown, catalog.DirDown, catalog.DirUp, catalog.DirLeft}},
	{Key: "shield-generator-pack.svg", NameZH: "SH-32 护盾生成包", NameEN: "SH-32 Shield Generator Pack", Category: catalog.CategoryBlue, Code: []catalog.Dir{catalog.DirDown, catalog.DirUp, catalog.DirLeft, catalog.DirRight, catalog.DirLeft, catalog.DirRight}},
	{Key: "directional-shield.svg", NameZH: "SH-51 定向护盾", NameEN: "SH-51 Directional Shield", Category: catalog.CategoryBlue, Code: []catalog.Dir{catalog.DirDown, catalog.DirUp, catalog.DirLeft, catalog.DirRight, catalog.DirUp, catalog.DirUp}},
	{Key: "guard-dog.svg", NameZH: "AX/AR-23“护卫犬”", NameEN: "AX/AR-23 “Guard Dog”", Category: catalog.CategoryBlue, Code: []catalog.Dir{catalog.DirDown, catalog.DirUp, catalog.DirLeft, catalog.DirUp, catalog.DirRight, catalog.DirDown}},
	{Key: "guard-dog-rover.svg", NameZH: "AX/LAS-5“护卫犬”漫游者", NameEN: "AX/LAS-5 “Guard Dog” Rover", Category: catalog.CategoryBlue, Code: []catalog.Dir{catalog.DirDown, catalog.DirUp, catalog.DirLeft, catalog.DirUp, catalog.DirRight, catalog.DirRight}},
	{Key: "guard-dog-breath.svg", NameZH: "AX/TX-13“护卫犬”毒息", NameEN: "AX/TX-13 “Guard Dog” Dog Breath", Category: catalog.CategoryBlue, Code: []catalog.Dir{catalog.DirDown, catalog.DirUp, catalog.DirLeft, catalog.DirUp, catalog.DirRight, catalog.DirUp}},
	{Key: "guard-dog-k-9.svg", NameZH: "AX/ARC-3“护卫犬”K-9", NameEN: "AX/ARC-3 “Guard Dog” K-9", Category: catalog.CategoryBlue, Code: []catalog.Dir{catalog.DirDown, catalog.DirUp, catalog.DirLeft, catalog.DirUp, catalog.DirRight, catalog.DirLeft}},
	{Key: "guard-dog-hot-dog.svg", NameZH: "AX/FLAM-75“护卫犬”热狗", NameEN: "AX/FLAM-75 “Guard Dog” Hot Dog", Category: catalog.CategoryBlue, Code: []catalog.Dir{catalog.DirDown, catalog.DirUp, catalog.DirLeft, catalog.DirUp, catalog.DirLeft, catalog.DirLeft}},
	{Key: "patriot-exosuit.svg", NameZH: "EXO-45“爱国者”外骨骼装甲", NameEN: "EXO-45 Patriot Exosuit", Category: catalog.CategoryBlue, Code: []catalog.Dir{catalog.DirLeft, catalog.DirDown, catalog.DirRight, catalog.DirUp, catalog.DirLeft, catalog.DirDown, catalog.DirDown}},
	{Key: "emancipator-exosuit.svg", NameZH: "EXO-49“解放者”外骨骼装甲", NameEN: "EXO-49 Emancipator Exosuit", Category: catalog.CategoryBlue, Code: []catalog.Dir{catalog.DirLeft, catalog.DirDown, catalog.DirRight, catalog.DirUp, catalog.DirLeft, catalog.DirDown, catalog.DirUp}},
	{Key: "lumberer-exosuit.svg", NameZH: "EXO-51“伐木工”外骨骼装甲", NameEN: "EXO-51 Lumberer Exosuit", Category: catalog.CategoryBlue, Code: []catalog.Dir{catalog.DirLeft, catalog.DirDown, catalog.DirRight, catalog.DirUp, catalog.DirRight, catalog.DirLeft, catalog.DirUp}},
	{Key: "breakthrough-exosuit.svg", NameZH: "“突围者”外骨骼装甲", NameEN: "Breakthrough Exosuit", Category: catalog.CategoryBlue, Code: []catalog.Dir{catalog.DirLeft, catalog.DirDown, catalog.DirRight, catalog.DirLeft, catalog.DirRight, catalog.DirDown, catalog.DirUp}},
	{Key: "fast-recon-vehicle.svg", NameZH: "M-102 快速侦察车", NameEN: "M-102 Fast Recon Vehicle", Category: catalog.CategoryBlue, Code: []catalog.Dir{catalog.DirLeft, catalog.DirDown, catalog.DirRight, catalog.DirDown, catalog.DirRight, catalog.DirDown, catalog.DirUp}},
	{Key: "supply-frv.svg", NameZH: "补给型快速侦察车", NameEN: "Supply FRV", Category: catalog.CategoryBlue, Code: []catalog.Dir{catalog.DirLeft, catalog.DirDown, catalog.DirLeft, catalog.DirLeft, catalog.DirDown, catalog.DirUp, catalog.DirRight}},
	{Key: "incinerator-frv.svg", NameZH: "M-104 焚烧型快速侦察车", NameEN: "M-104 Incinerator FRV", Category: catalog.CategoryBlue, Code: []catalog.Dir{catalog.DirLeft, catalog.DirDown, catalog.DirRight, catalog.DirLeft, catalog.DirDown, catalog.DirUp}},
	{Key: "bastion-mk-xvi.svg", NameZH: "TD-220“堡垒”MK XVI 坦克", NameEN: "TD-220 Bastion MK XVI", Category: catalog.CategoryBlue, Code: []catalog.Dir{catalog.DirLeft, catalog.DirDown, catalog.DirRight, catalog.DirDown, catalog.DirLeft, catalog.DirDown, catalog.DirUp, catalog.DirDown, catalog.DirUp}},
	{Key: "td-110-maelstrom.svg", NameZH: "TD-110“漩涡”重型坦克", NameEN: "TD-110 Maelstrom", Category: catalog.CategoryBlue, Code: []catalog.Dir{catalog.DirLeft, catalog.DirDown, catalog.DirRight, catalog.DirDown, catalog.DirLeft, catalog.DirDown, catalog.DirUp, catalog.DirLeft, catalog.DirRight}},

	// Defensive Sentries, Emplacements & Mines (Green)
	{Key: "machine-gun-sentry.svg", NameZH: "A/MG-43 机枪哨戒炮", NameEN: "A/MG-43 Machine Gun Sentry", Category: catalog.CategoryGreen, Code: []catalog.Dir{catalog.DirDown, catalog.DirUp, catalog.DirRight, catalog.DirRight, catalog.DirUp}},
	{Key: "gatling-sentry.svg", NameZH: "A/G-16 加特林哨戒炮", NameEN: "A/G-16 Gatling Sentry", Category: catalog.CategoryGreen, Code: []catalog.Dir{catalog.DirDown, catalog.DirUp, catalog.DirRight, catalog.DirLeft}},
	{Key: "mortar-sentry.svg", NameZH: "A/M-12 迫击炮哨戒炮", NameEN: "A/M-12 Mortar Sentry", Category: catalog.CategoryGreen, Code: []catalog.Dir{catalog.DirDown, catalog.DirUp, catalog.DirRight, catalog.DirRight, catalog.DirDown}},
	{Key: "autocannon-sentry.svg", NameZH: "A/AC-8 机炮哨戒炮", NameEN: "A/AC-8 Autocannon Sentry", Category: catalog.CategoryGreen, Code: []catalog.Dir{catalog.DirDown, catalog.DirUp, catalog.DirRight, catalog.DirUp, catalog.DirLeft, catalog.DirUp}},
	{Key: "rocket-sentry.svg", NameZH: "A/MLS-4X 火箭哨戒炮", NameEN: "A/MLS-4X Rocket Sentry", Category: catalog.CategoryGreen, Code: []catalog.Dir{catalog.DirDown, catalog.DirUp, catalog.DirRight, catalog.DirRight, catalog.DirLeft}},
	{Key: "ems-mortar-sentry.svg", NameZH: "A/M-23 电磁冲击波迫击炮哨戒炮", NameEN: "A/M-23 EMS Mortar Sentry", Category: catalog.CategoryGreen, Code: []catalog.Dir{catalog.DirDown, catalog.DirUp, catalog.DirRight, catalog.DirDown, catalog.DirRight}},
	{Key: "flame-sentry.svg", NameZH: "A/FLAM-40 火焰哨戒炮", NameEN: "A/FLAM-40 Flame Sentry", Category: catalog.CategoryGreen, Code: []catalog.Dir{catalog.DirDown, catalog.DirUp, catalog.DirRight, catalog.DirDown, catalog.DirUp, catalog.DirUp}},
	{Key: "laser-sentry.svg", NameZH: "A/LAS-98 激光哨戒炮", NameEN: "A/LAS-98 Laser Sentry", Category: catalog.CategoryGreen, Code: []catalog.Dir{catalog.DirDown, catalog.DirUp, catalog.DirRight, catalog.DirDown, catalog.DirUp, catalog.DirRight}},
	{Key: "gas-mortar-sentry.svg", NameZH: "毒气迫击炮哨戒炮", NameEN: "Gas Mortar Sentry", Category: catalog.CategoryGreen, Code: []catalog.Dir{catalog.DirDown, catalog.DirUp, catalog.DirRight, catalog.DirDown, catalog.DirLeft}},
	{Key: "hmg-emplacement.svg", NameZH: "E/MG-101 重机枪部署支架", NameEN: "E/MG-101 HMG Emplacement", Category: catalog.CategoryGreen, Code: []catalog.Dir{catalog.DirDown, catalog.DirUp, catalog.DirLeft, catalog.DirRight, catalog.DirRight, catalog.DirLeft}},
	{Key: "anti-tank-emplacement.svg", NameZH: "E/AT-12 反坦克部署支架", NameEN: "E/AT-12 Anti-Tank Emplacement", Category: catalog.CategoryGreen, Code: []catalog.Dir{catalog.DirDown, catalog.DirUp, catalog.DirLeft, catalog.DirRight, catalog.DirRight, catalog.DirRight}},
	{Key: "grenadier-battlement.svg", NameZH: "E/GL-21 掷弹兵防卫墙", NameEN: "E/GL-21 Grenadier Battlement", Category: catalog.CategoryGreen, Code: []catalog.Dir{catalog.DirDown, catalog.DirRight, catalog.DirDown, catalog.DirLeft, catalog.DirRight}},
	{Key: "shield-generator-relay.svg", NameZH: "FX-12 护盾生成中继器", NameEN: "FX-12 Shield Generator Relay", Category: catalog.CategoryGreen, Code: []catalog.Dir{catalog.DirDown, catalog.DirDown, catalog.DirLeft, catalog.DirRight, catalog.DirLeft, catalog.DirRight}},
	{Key: "tesla-tower.svg", NameZH: "A/ARC-3 特斯拉塔", NameEN: "A/ARC-3 Tesla Tower", Category: catalog.CategoryGreen, Code: []catalog.Dir{catalog.DirDown, catalog.DirUp, catalog.DirRight, catalog.DirUp, catalog.DirLeft, catalog.DirRight}},
	{Key: "anti-personnel-minefield.svg", NameZH: "MD-6 反步兵雷区", NameEN: "MD-6 Anti-Personnel Minefield", Category: catalog.CategoryGreen, Code: []catalog.Dir{catalog.DirDown, catalog.DirLeft, catalog.DirUp, catalog.DirRight}},
	{Key: "incendiary-mines.svg", NameZH: "MD-I4 燃烧地雷", NameEN: "MD-I4 Incendiary Mines", Category: catalog.CategoryGreen, Code: []catalog.Dir{catalog.DirDown, catalog.DirLeft, catalog.DirLeft, catalog.DirDown}},
	{Key: "anti-tank-mines.svg", NameZH: "MD-17 反坦克地雷", NameEN: "MD-17 Anti-Tank Mines", Category: catalog.CategoryGreen, Code: []catalog.Dir{catalog.DirDown, catalog.DirLeft, catalog.DirUp, catalog.DirUp}},
	{Key: "gas-mine.svg", NameZH: "MD-8 毒气地雷", NameEN: "MD-8 Gas Mines", Category: catalog.CategoryGreen, Code: []catalog.Dir{catalog.DirDown, catalog.DirLeft, catalog.DirLeft, catalog.DirRight}},

	// Mission & General Stratagems (Yellow)
	{Key: "resupply.svg", NameZH: "重新补给", NameEN: "Resupply", Category: catalog.CategoryYellow, Code: []catalog.Dir{catalog.DirDown, catalog.DirDown, catalog.DirUp, catalog.DirRight}},
	{Key: "reinforce.svg", NameZH: "增援", NameEN: "Reinforce", Category: catalog.CategoryYellow, Code: []catalog.Dir{catalog.DirUp, catalog.DirDown, catalog.DirRight, catalog.DirLeft, catalog.DirUp}},
	{Key: "sos.svg", NameZH: "SOS 求救信标", NameEN: "SOS Beacon", Category: catalog.CategoryYellow, Code: []catalog.Dir{catalog.DirUp, catalog.DirDown, catalog.DirRight, catalog.DirUp}},
	{Key: "eagle-rearm.svg", NameZH: "“飞鹰”重新武装", NameEN: "Eagle Rearm", Category: catalog.CategoryYellow, Code: []catalog.Dir{catalog.DirUp, catalog.DirUp, catalog.DirLeft, catalog.DirUp, catalog.DirRight}},
	{Key: "hellbomb.svg", NameZH: "NUX-223 地狱火炸弹", NameEN: "NUX-223 Hellbomb", Category: catalog.CategoryYellow, Code: []catalog.Dir{catalog.DirDown, catalog.DirUp, catalog.DirLeft, catalog.DirDown, catalog.DirUp, catalog.DirRight, catalog.DirDown, catalog.DirUp}},
	{Key: "seaf-artillery.svg", NameZH: "超级地球武装部队大炮", NameEN: "SEAF Artillery", Category: catalog.CategoryYellow, Code: []catalog.Dir{catalog.DirRight, catalog.DirUp, catalog.DirUp, catalog.DirDown}},
	{Key: "super-earth-flag.svg", NameZH: "超级地球旗帜", NameEN: "Super Earth Flag", Category: catalog.CategoryYellow, Code: []catalog.Dir{catalog.DirDown, catalog.DirUp, catalog.DirDown, catalog.DirUp}},
	{Key: "upload-data.svg", NameZH: "上传数据", NameEN: "Upload Data", Category: catalog.CategoryYellow, Code: []catalog.Dir{catalog.DirLeft, catalog.DirRight, catalog.DirUp, catalog.DirUp, catalog.DirUp}},
	{Key: "seismic-probe.svg", NameZH: "地质探测仪", NameEN: "Seismic Probe", Category: catalog.CategoryYellow, Code: []catalog.Dir{catalog.DirUp, catalog.DirUp, catalog.DirLeft, catalog.DirRight, catalog.DirDown, catalog.DirDown}},
	{Key: "prospecting-drill.svg", NameZH: "勘探钻机", NameEN: "Prospecting Drill", Category: catalog.CategoryYellow, Code: []catalog.Dir{catalog.DirDown, catalog.DirDown, catalog.DirLeft, catalog.DirRight, catalog.DirDown, catalog.DirDown}},
	{Key: "tectonic-drill.svg", NameZH: "地壳钻机", NameEN: "Tectonic Drill", Category: catalog.CategoryYellow, Code: []catalog.Dir{catalog.DirUp, catalog.DirDown, catalog.DirUp, catalog.DirDown, catalog.DirUp, catalog.DirDown}},
	{Key: "hive-breaker-drill.svg", NameZH: "虫巢破坏钻机", NameEN: "Hive Breaker Drill", Category: catalog.CategoryYellow, Code: []catalog.Dir{catalog.DirLeft, catalog.DirUp, catalog.DirDown, catalog.DirRight, catalog.DirDown, catalog.DirDown}},
	{Key: "dark-fluid-vessel.svg", NameZH: "暗流体容器", NameEN: "Dark Fluid Vessel", Category: catalog.CategoryYellow, Code: []catalog.Dir{catalog.DirUp, catalog.DirLeft, catalog.DirRight, catalog.DirDown, catalog.DirUp, catalog.DirUp}},
	{Key: "orbital-illumination-flare.svg", NameZH: "轨道照明弹", NameEN: "Orbital Illumination Flare", Category: catalog.CategoryYellow, Code: []catalog.Dir{catalog.DirRight, catalog.DirRight, catalog.DirLeft, catalog.DirLeft}},
	{Key: "cargo-container.svg", NameZH: "货运集装箱", NameEN: "Cargo Container", Category: catalog.CategoryYellow, Code: []catalog.Dir{catalog.DirUp, catalog.DirUp, catalog.DirDown, catalog.DirDown, catalog.DirRight, catalog.DirDown}},
	{Key: "call-in-super-destroyer.svg", NameZH: "呼叫超级驱逐舰", NameEN: "Call In Super Destroyer", Category: catalog.CategoryYellow, Code: []catalog.Dir{catalog.DirUp, catalog.DirUp, catalog.DirDown, catalog.DirDown, catalog.DirLeft, catalog.DirRight, catalog.DirLeft, catalog.DirRight}},
}

// FindIcon looks up IconMeta by key.
func FindIcon(key string) (IconMeta, bool) {
	for _, ic := range Icons {
		if ic.Key == key {
			return ic, true
		}
	}
	return IconMeta{}, false
}

// IconName returns the localized display name for an icon key, falling back to
// a cleaned title only if an unknown custom icon key is encountered.
func IconName(key string, lang catalog.Language) string {
	if ic, ok := FindIcon(key); ok {
		return ic.Name(lang)
	}
	base := strings.TrimSuffix(path.Base(key), path.Ext(key))
	base = strings.ReplaceAll(base, "-", " ")
	if base == "" {
		if lang == catalog.LangEN {
			return "Orbital Precision Strike"
		}
		return "轨道精准攻击"
	}
	return base
}

// SystemTacticMeta describes a zero-config built-in window or OS tactical directive.
type SystemTacticMeta struct {
	IsCloseWindow bool
	Action        catalog.SystemAction
	IconKey       string
	Glyph         string
	NameZH        string
	NameEN        string
}

func (m SystemTacticMeta) Name(lang catalog.Language) string {
	if lang == catalog.LangEN {
		return m.NameEN
	}
	return m.NameZH
}

// SystemTactics lists all built-in native desktop window & system directives.
var SystemTactics = []SystemTacticMeta{
	{IsCloseWindow: true, IconKey: "close_window", Glyph: "✕", NameZH: "关闭当前窗口", NameEN: "Close Active Window"},
	{Action: catalog.SysWinLeft, IconKey: "win_left", Glyph: "◧", NameZH: "窗口贴靠左半屏", NameEN: "Tile Window Left"},
	{Action: catalog.SysWinRight, IconKey: "win_right", Glyph: "◨", NameZH: "窗口贴靠右半屏", NameEN: "Tile Window Right"},
	{Action: catalog.SysWinMaximize, IconKey: "win_maximize", Glyph: "□", NameZH: "窗口最大化 / 还原", NameEN: "Maximize / Restore"},
	{Action: catalog.SysWinCenter, IconKey: "win_center", Glyph: "⊡", NameZH: "窗口居中 (72%)", NameEN: "Center Window"},
	{Action: catalog.SysWinMinimize, IconKey: "win_minimize", Glyph: "▁", NameZH: "最小化当前窗口", NameEN: "Minimize Window"},
	{Action: catalog.SysShowDesktop, IconKey: "show_desktop", Glyph: "▣", NameZH: "显示桌面", NameEN: "Show Desktop"},
	{Action: catalog.SysScreenshot, IconKey: "screenshot", Glyph: "⌖", NameZH: "区域截图至剪贴板", NameEN: "Capture Region"},
	{Action: catalog.SysLockScreen, IconKey: "lock_screen", Glyph: "◈", NameZH: "立即锁定屏幕", NameEN: "Lock Screen"},
	{Action: catalog.SysMediaPlayPause, IconKey: "media_play", Glyph: "▶", NameZH: "媒体播放 / 暂停", NameEN: "Play / Pause Media"},
	{Action: catalog.SysVolumeMute, IconKey: "volume_mute", Glyph: "✕", NameZH: "系统静音切换", NameEN: "Toggle System Mute"},
	{Action: catalog.SysTaskManager, IconKey: "task_manager", Glyph: "∿", NameZH: "任务管理器", NameEN: "Task Monitor"},
	{Action: catalog.SysEmptyTrash, IconKey: "empty_trash", Glyph: "⌫", NameZH: "清空回收站", NameEN: "Empty Trash"},
}

// SystemActionName returns the localized display name for a catalog.SystemAction.
func SystemActionName(act catalog.SystemAction, lang catalog.Language) string {
	for _, t := range SystemTactics {
		if !t.IsCloseWindow && t.Action == act {
			return t.Name(lang)
		}
	}
	return string(act)
}

// Strings holds concise localized UI strings for a single language.
type Strings struct {
	// Window & Navigation
	WindowTitle    string
	AppTitle       string
	NavStratagems  string
	NavAppearance  string
	NavSettings    string
	LoadoutHeader  string
	NewStratagem   string
	DefaultNewName string

	// Appearance & General Settings
	SectionTheme          string
	ThemeClassicTag       string
	ThemeTacticalTag      string
	SectionDisplay        string
	Visor3DLabel          string
	DesktopPreviewLabel   string
	PreviewDesktopBtn     string
	SectionControls       string
	SummonShortcutLabel   string
	TriggerDoubleCtrl     string
	TriggerLeftCtrl       string
	TriggerRightCtrl      string
	SoundSettingLabel     string
	GameBypassLabel       string
	SectionSystem         string
	LanguageLabel         string
	LaunchAtLoginLabel    string
	ShipNameLabel         string
	ShipRandomBtn         string
	ShipPlaceholder       string
	OnboardingReplayLabel string
	OnboardingReplayBtn   string
	ConfigBackupLabel     string
	ExportConfigBtn       string
	ImportConfigBtn       string
	ExportDialogTitle     string
	ImportDialogTitle     string
	PermissionLabel       string
	PermissionGranted     string
	PermissionMissing     string
	PermissionGrantBtn    string
	LogsLabel             string
	OpenLogsBtn           string
	ResetLoadoutLabel     string
	ResetDefaults         string
	ConfirmReset          string
	Cancel                string

	// Editor Controls & Icon Picker
	NamePlaceholderWin     string
	NamePlaceholderOther   string
	PickIcon               string
	SearchIconsPlaceholder string
	CatAll                 string
	CatOffensive           string
	CatSupply              string
	CatDefensive           string
	CatMission             string
	UseOfficialCodeBtn     string
	DeliveryImmediate      string
	DeliveryBeacon         string
	RecordSequence         string
	StopRecording          string
	SequencePlaceholder    string
	RecordingPlaceholder   string
	ClearSequence          string

	// Action Kinds & Multi-Step Pipeline
	StepApp             string
	StepOpen            string
	StepShell           string
	StepShortcut        string
	StepClose           string
	StepSystem          string
	AddComboStep        string
	StepBadgeFmt        string
	RemoveStep          string
	InsertClipboardChip string

	TargetAppWin        string
	TargetAppOther      string
	TargetOpen          string
	TargetShellWin      string
	TargetShellOther    string
	TargetShortcutWin   string
	TargetShortcutOther string
	FilterApps          string

	Save   string
	Revert string
	Delete string

	SavedToast    string
	DeletedToast  string
	ResetToast    string
	ExportedToast string
	ImportedToast string

	// System Stratagem: Join the Helldivers (Enlistment Terminal)
	SystemBadge            string
	EnlistBadge            string
	EnlistLockedCodeLabel  string
	EnlistLockedTag        string
	EnlistCoordsTitle      string
	EnlistStatusSteam      string
	EnlistStatusCustom     string
	EnlistStatusBroken     string
	EnlistStatusMissing    string
	EnlistBtnRescan        string
	EnlistBtnBrowse        string
	EnlistBtnChange        string
	EnlistBtnAutoDetect    string
	EnlistBtnDiveNow       string
	EnlistBtnSteamStore    string
	EnlistDialogTitle      string
	EnlistScanFoundToast   string
	EnlistScanMissingToast string

	// Live Preview Pane
	PreviewTitle  string
	ThemeClassic  string
	ThemeTactical string
	SimReady      string
	SimTyping     string
	SimDeploying  string

	// HUD Overlay Strings
	HUDTitle              string
	HUDBadgeError         string
	HUDBadgeArmed         string
	HUDBadgeDeploying     string
	HUDBadgeDeployed      string
	HUDBadgePreview       string
	HUDInputCountFmt      string
	HUDUntrustedMac       string
	HUDArmedHint          string
	HUDCallingIn          string
	HUDClosingWindow      string
	HUDHellpodLaunching   string
	HUDSystemOffline      string
	HUDSystemOfflineBadge string
	HUDDeployedShort      string
	HUDEmptyLoadout       string
	HUDInputLabel         string
	HUDWaitingInput       string
	HUDFooterArmed        string
	HUDFooterDeploy       string
	HUDFooterDone         string
	HUDFooterHoldLeft     string
	HUDFooterRightAlt     string
	HUDFooterDouble       string
	HUDFooterCancel       string

	// System Tray Menu
	TrayReady          string
	TrayPausedStatus   string
	TrayNeedPermission string
	TrayOpenArsenal    string
	TrayPreviewHUD     string
	TrayPauseHotkeys   string
	TrayResumeHotkeys  string
	TrayOnboarding     string
	TrayQuit           string

	// First-Run Onboarding (Zero-Description, Single-Focus Steps)
	OnbWindowTitle      string
	OnbSkipVideoBtn     string
	OnbSkipAll          string
	OnbEnlistNowBtn     string
	OnbStepThemeTitle   string
	OnbStepTriggerTitle string
	OnbStepLoadoutTitle string
	OnbStepDrillTitle   string
	OnbRecBadge         string
	OnbTrigDouble       string
	OnbTrigLeft         string
	OnbTrigRight        string
	OnbPodCloseWin      string
	OnbPodFiles         string
	OnbPodSplit         string
	OnbPodBrowser       string
	OnbPodScreenshot    string
	OnbPodLock          string
	OnbDrillReady       string
	OnbBackBtn          string
	OnbNextBtn          string
	OnbFinishDiveBtn    string
}

var zhStrings = Strings{
	WindowTitle:    "战略配备军械库 — HelLauncher",
	AppTitle:       "战略配备军械库",
	NavStratagems:  "战备配置",
	NavAppearance:  "面板外观",
	NavSettings:    "通用设置",
	LoadoutHeader:  "战略配备",
	NewStratagem:   "+ 新建",
	DefaultNewName: "新建战略配备",

	SectionTheme:          "面板主题",
	ThemeClassicTag:       "高斯毛玻璃",
	ThemeTacticalTag:      "头盔战术目镜",
	SectionDisplay:        "显示效果",
	Visor3DLabel:          "3D 头盔弧面投射",
	DesktopPreviewLabel:   "桌面实机悬浮窗",
	PreviewDesktopBtn:     "弹出预览",
	SectionControls:       "操控与反馈",
	SummonShortcutLabel:   "唤出快捷键",
	TriggerDoubleCtrl:     "双击 Ctrl",
	TriggerLeftCtrl:       "左 Ctrl",
	TriggerRightCtrl:      "右 Ctrl / Alt",
	SoundSettingLabel:     "指令按键音效",
	GameBypassLabel:       "全屏与游戏自动避让",
	SectionSystem:         "通用与系统",
	LanguageLabel:         "界面语言",
	LaunchAtLoginLabel:    "开机自动待命",
	ShipNameLabel:         "超级驱逐舰舰名",
	ShipRandomBtn:         "随机授舰",
	ShipPlaceholder:       "如 SES 家庭价值观之拳号",
	OnboardingReplayLabel: "真理部入列仪式",
	OnboardingReplayBtn:   "重温入列引导…",
	ConfigBackupLabel:     "战备档案备份",
	ExportConfigBtn:       "导出 JSON…",
	ImportConfigBtn:       "导入 JSON…",
	ExportDialogTitle:     "导出 HelLauncher 战备档案",
	ImportDialogTitle:     "导入 HelLauncher 战备档案",
	PermissionLabel:       "辅助功能权限",
	PermissionGranted:     "已授权",
	PermissionMissing:     "未授权",
	PermissionGrantBtn:    "授权引导…",
	LogsLabel:             "运行日志",
	OpenLogsBtn:           "打开目录",
	ResetLoadoutLabel:     "默认战备数据",
	ResetDefaults:         "恢复默认战备",
	ConfirmReset:          "确认恢复默认",
	Cancel:                "取消",

	NamePlaceholderWin:     "战略配备名称（如 Windows Terminal）",
	NamePlaceholderOther:   "战略配备名称（如 Ghostty）",
	PickIcon:               "选择图标",
	SearchIconsPlaceholder: "搜索全部 111 项官方战略配备…",
	CatAll:                 "全部",
	CatOffensive:           "进攻",
	CatSupply:              "支援",
	CatDefensive:           "防御",
	CatMission:             "任务",
	UseOfficialCodeBtn:     "套用原版指令",
	DeliveryImmediate:      "立即执行",
	DeliveryBeacon:         "⊕ 鼠标落点",
	RecordSequence:         "录制按键",
	StopRecording:          "完成",
	SequencePlaceholder:    "点击录制或按右侧方向键",
	RecordingPlaceholder:   "按 WASD / ↑↓←→ 输入，Enter 完成",
	ClearSequence:          "清空",

	StepApp:             "应用",
	StepOpen:            "打开",
	StepShell:           "命令",
	StepShortcut:        "脚本",
	StepClose:           "关窗",
	StepSystem:          "系统",
	AddComboStep:        "+ 连携动作",
	StepBadgeFmt:        "步骤 %d",
	RemoveStep:          "移除",
	InsertClipboardChip: "+ {clipboard} 剪贴板",

	TargetAppWin:        "应用名、可执行路径或预设（如 files）",
	TargetAppOther:      "Bundle ID、路径或预设（如 files）",
	TargetOpen:          "https://google.com/search?q={clipboard} 或 ~/Downloads",
	TargetShellWin:      `wt -d D:\development`,
	TargetShellOther:    "open -a Simulator",
	TargetShortcutWin:   ".lnk 快捷方式或 PowerShell 脚本",
	TargetShortcutOther: "macOS 快捷指令名称",
	FilterApps:          "搜索本机应用…",

	Save:   "保存",
	Revert: "还原",
	Delete: "删除",

	SavedToast:    "✓ 已保存",
	DeletedToast:  "✓ 已删除",
	ResetToast:    "✓ 已恢复默认战备",
	ExportedToast: "✓ 战备档案已导出",
	ImportedToast: "✓ 战备档案已导入",

	SystemBadge:            "系统",
	EnlistBadge:            "真理部钦定 · 系统常驻",
	EnlistLockedCodeLabel:  "固定空投指令",
	EnlistLockedTag:        "不可更改",
	EnlistCoordsTitle:      "空投舱发射井坐标",
	EnlistStatusSteam:      "● 已锁定超级驱逐舰发射井（Steam 自动探测）",
	EnlistStatusCustom:     "● 已挂载自定义空投坐标",
	EnlistStatusBroken:     "⚠ 指定的启动文件不存在或已被转移",
	EnlistStatusMissing:    "○ 本机未探测到《绝地潜兵 2》空投发射井",
	EnlistBtnRescan:        "重新扫描",
	EnlistBtnBrowse:        "手动选择启动程序…",
	EnlistBtnChange:        "更改位置…",
	EnlistBtnAutoDetect:    "恢复自动探测",
	EnlistBtnDiveNow:       "立即潜入游戏",
	EnlistBtnSteamStore:    "✪ 前往超级地球征兵处 (Steam)",
	EnlistDialogTitle:      "选择《绝地潜兵 2》启动程序或快捷方式",
	EnlistScanFoundToast:   "✓ 已锁定绝地潜兵空投发射井",
	EnlistScanMissingToast: "未在本地默认库探测到客户端，请手动选择启动位置",

	PreviewTitle:  "实时预览",
	ThemeClassic:  "原版",
	ThemeTactical: "战术",
	SimReady:      "待命",
	SimTyping:     "输入中",
	SimDeploying:  "部署中",

	HUDTitle:              "战略配备",
	HUDBadgeError:         "指令错误",
	HUDBadgeArmed:         "信标已握持",
	HUDBadgeDeploying:     "空投部署中",
	HUDBadgeDeployed:      "✓ 已就绪",
	HUDBadgePreview:       "终端就绪",
	HUDInputCountFmt:      "输入中 %d",
	HUDUntrustedMac:       "需要辅助功能权限：请在军械库「通用设置」中点击「授权引导…」",
	HUDArmedHint:          "信标已握持 · 左键点击目标位置",
	HUDCallingIn:          "正在呼叫支援…",
	HUDClosingWindow:      "正在轨道精准清场…",
	HUDHellpodLaunching:   "空投舱发射中 · 为了超级地球！",
	HUDSystemOffline:      "未定位发射井 · ↑↓→←↑ 配置",
	HUDSystemOfflineBadge: "待部署",
	HUDDeployedShort:      "✓ 已就绪",
	HUDEmptyLoadout:       "暂无启用战备",
	HUDInputLabel:         "指令:",
	HUDWaitingInput:       "WASD / ↑↓←→",
	HUDFooterArmed:        "左键空投 · 右键取消",
	HUDFooterDeploy:       "部署中",
	HUDFooterDone:         "已就绪",
	HUDFooterHoldLeft:     "按住左 CTRL",
	HUDFooterRightAlt:     "右 CTRL / CTRL+ALT",
	HUDFooterDouble:       "双击按住 CTRL",
	HUDFooterCancel:       "松开取消 · ESC",

	TrayReady:          "就绪",
	TrayPausedStatus:   "⏸ 热键已暂停",
	TrayNeedPermission: "⚠️ 待授权限",
	TrayOpenArsenal:    "战略配备军械库…",
	TrayPreviewHUD:     "预览面板",
	TrayPauseHotkeys:   "暂停热键监听",
	TrayResumeHotkeys:  "▶ 恢复热键监听",
	TrayOnboarding:     "✪ 入列引导仪式…",
	TrayQuit:           "退出 HelLauncher",

	OnbWindowTitle:      "绝地潜兵入列终端 — HelLauncher",
	OnbSkipVideoBtn:     "跳过播片",
	OnbSkipAll:          "跳过",
	OnbEnlistNowBtn:     "按任意键入列",
	OnbStepThemeTitle:   "面板主题",
	OnbStepTriggerTitle: "唤出热键",
	OnbStepLoadoutTitle: "出舱战备",
	OnbStepDrillTitle:   "呼叫文件补给箱",
	OnbRecBadge:         "推荐",
	OnbTrigDouble:       "双击按住 Ctrl",
	OnbTrigLeft:         "按住左 Ctrl",
	OnbTrigRight:        "按住右 Ctrl",
	OnbPodCloseWin:      "关闭当前窗口",
	OnbPodFiles:         "文件补给箱",
	OnbPodSplit:         "窗口左 / 右分屏",
	OnbPodBrowser:       "默认浏览器",
	OnbPodScreenshot:    "区域截图",
	OnbPodLock:          "立即锁屏",
	OnbDrillReady:       "✓ 准备就绪",
	OnbBackBtn:          "上一步",
	OnbNextBtn:          "下一步",
	OnbFinishDiveBtn:    "开始执勤",
}

var enStrings = Strings{
	WindowTitle:    "Stratagem Arsenal — HelLauncher",
	AppTitle:       "Stratagem Arsenal",
	NavStratagems:  "Stratagems",
	NavAppearance:  "Appearance",
	NavSettings:    "Settings",
	LoadoutHeader:  "Stratagems",
	NewStratagem:   "+ New",
	DefaultNewName: "New Stratagem",

	SectionTheme:          "HUD Theme",
	ThemeClassicTag:       "Frosted Glass",
	ThemeTacticalTag:      "Helmet Visor",
	SectionDisplay:        "Display",
	Visor3DLabel:          "3D Visor Projection",
	DesktopPreviewLabel:   "Desktop Overlay",
	PreviewDesktopBtn:     "Pop-out Preview",
	SectionControls:       "Controls & Audio",
	SummonShortcutLabel:   "Summon Shortcut",
	TriggerDoubleCtrl:     "2× Hold Ctrl",
	TriggerLeftCtrl:       "Left Ctrl",
	TriggerRightCtrl:      "Right Ctrl / Alt",
	SoundSettingLabel:     "Keypress Sound",
	GameBypassLabel:       "Auto-Pause in Fullscreen Games",
	SectionSystem:         "General & System",
	LanguageLabel:         "Language",
	LaunchAtLoginLabel:    "Launch at Login",
	ShipNameLabel:         "Super Destroyer Name",
	ShipRandomBtn:         "Roll Name",
	ShipPlaceholder:       "e.g. SES Fist of Family Values",
	OnboardingReplayLabel: "Enlistment Intro",
	OnboardingReplayBtn:   "Replay Intro…",
	ConfigBackupLabel:     "Loadout Backup",
	ExportConfigBtn:       "Export JSON…",
	ImportConfigBtn:       "Import JSON…",
	ExportDialogTitle:     "Export HelLauncher Loadout",
	ImportDialogTitle:     "Import HelLauncher Loadout",
	PermissionLabel:       "Accessibility",
	PermissionGranted:     "Granted",
	PermissionMissing:     "Required",
	PermissionGrantBtn:    "Grant…",
	LogsLabel:             "Diagnostics Logs",
	OpenLogsBtn:           "Open Folder",
	ResetLoadoutLabel:     "Default Loadout",
	ResetDefaults:         "Reset Defaults",
	ConfirmReset:          "Confirm Reset",
	Cancel:                "Cancel",

	NamePlaceholderWin:     "Stratagem name (e.g. Windows Terminal)",
	NamePlaceholderOther:   "Stratagem name (e.g. Ghostty)",
	PickIcon:               "Choose Icon",
	SearchIconsPlaceholder: "Search all 111 official stratagems…",
	CatAll:                 "All",
	CatOffensive:           "Offensive",
	CatSupply:              "Supply",
	CatDefensive:           "Defensive",
	CatMission:             "Mission",
	UseOfficialCodeBtn:     "Official Code",
	DeliveryImmediate:      "Instant",
	DeliveryBeacon:         "⊕ Click Target",
	RecordSequence:         "Record",
	StopRecording:          "Done",
	SequencePlaceholder:    "Click Record or tap arrows",
	RecordingPlaceholder:   "Press WASD / ↑↓←→ · Enter to finish",
	ClearSequence:          "Clear",

	StepApp:             "App",
	StepOpen:            "Open",
	StepShell:           "Shell",
	StepShortcut:        "Script",
	StepClose:           "Close Win",
	StepSystem:          "System",
	AddComboStep:        "+ Combo Step",
	StepBadgeFmt:        "Step %d",
	RemoveStep:          "Remove",
	InsertClipboardChip: "+ {clipboard}",

	TargetAppWin:        "App ID, path, or preset (e.g. files)",
	TargetAppOther:      "Bundle ID, path, or preset (e.g. files)",
	TargetOpen:          "https://google.com/search?q={clipboard} or ~/Downloads",
	TargetShellWin:      `wt -d D:\development`,
	TargetShellOther:    "open -a Simulator",
	TargetShortcutWin:   ".lnk shortcut or PowerShell script",
	TargetShortcutOther: "macOS Shortcut name",
	FilterApps:          "Filter installed apps…",

	Save:   "Save",
	Revert: "Revert",
	Delete: "Delete",

	SavedToast:    "✓ Saved",
	DeletedToast:  "✓ Deleted",
	ResetToast:    "✓ Defaults restored",
	ExportedToast: "✓ Loadout exported",
	ImportedToast: "✓ Loadout imported",

	SystemBadge:            "SYS",
	EnlistBadge:            "MINISTRY OF TRUTH · SYSTEM",
	EnlistLockedCodeLabel:  "Fixed Hellpod Sequence",
	EnlistLockedTag:        "LOCKED",
	EnlistCoordsTitle:      "Hellpod Launch Coordinates",
	EnlistStatusSteam:      "● Super Destroyer Silo Locked (Steam Auto-Detected)",
	EnlistStatusCustom:     "● Custom Hellpod Coordinates Active",
	EnlistStatusBroken:     "⚠ Specified launch file is missing or moved",
	EnlistStatusMissing:    "○ HELLDIVERS™ 2 Not Detected on This Machine",
	EnlistBtnRescan:        "Rescan",
	EnlistBtnBrowse:        "Locate Game…",
	EnlistBtnChange:        "Change…",
	EnlistBtnAutoDetect:    "Auto-Detect",
	EnlistBtnDiveNow:       "Dive Now",
	EnlistBtnSteamStore:    "✪ Enlist on Steam",
	EnlistDialogTitle:      "Select HELLDIVERS™ 2 Executable or Shortcut",
	EnlistScanFoundToast:   "✓ Hellpod silo locked",
	EnlistScanMissingToast: "Game not found in standard libraries",

	PreviewTitle:  "Live Preview",
	ThemeClassic:  "Classic",
	ThemeTactical: "Tactical",
	SimReady:      "Ready",
	SimTyping:     "Typing",
	SimDeploying:  "Calling",

	HUDTitle:              "STRATAGEMS",
	HUDBadgeError:         "INVALID",
	HUDBadgeArmed:         "BEACON ARMED",
	HUDBadgeDeploying:     "CALLING IN",
	HUDBadgeDeployed:      "✓ DEPLOYED",
	HUDBadgePreview:       "READY",
	HUDInputCountFmt:      "INPUT %d",
	HUDUntrustedMac:       "Accessibility permission required: open Arsenal → Settings → Grant…",
	HUDArmedHint:          "BEACON READY · LEFT-CLICK TARGET",
	HUDCallingIn:          "CALLING IN…",
	HUDClosingWindow:      "ORBITAL STRIKE INBOUND…",
	HUDHellpodLaunching:   "HELLPOD LAUNCHING · FOR SUPER EARTH!",
	HUDSystemOffline:      "SILO OFFLINE · ↑↓→←↑ LOCATE",
	HUDSystemOfflineBadge: "OFFLINE",
	HUDDeployedShort:      "✓ DEPLOYED",
	HUDEmptyLoadout:       "No active stratagems",
	HUDInputLabel:         "INPUT:",
	HUDWaitingInput:       "WASD / ↑↓←→",
	HUDFooterArmed:        "Click to drop · Right-click cancel",
	HUDFooterDeploy:       "Inbound",
	HUDFooterDone:         "Deployed",
	HUDFooterHoldLeft:     "Hold Left CTRL",
	HUDFooterRightAlt:     "Right CTRL / CTRL+ALT",
	HUDFooterDouble:       "Double-tap & hold CTRL",
	HUDFooterCancel:       "Release to cancel · ESC",

	TrayReady:          "Ready",
	TrayPausedStatus:   "⏸ Hotkeys Paused",
	TrayNeedPermission: "⚠️ Permission Needed",
	TrayOpenArsenal:    "Open Arsenal…",
	TrayPreviewHUD:     "Preview HUD",
	TrayPauseHotkeys:   "Pause Hotkeys",
	TrayResumeHotkeys:  "▶ Resume Hotkeys",
	TrayOnboarding:     "✪ Enlistment Intro…",
	TrayQuit:           "Quit HelLauncher",

	OnbWindowTitle:      "Helldiver Enlistment — HelLauncher",
	OnbSkipVideoBtn:     "SKIP INTRO",
	OnbSkipAll:          "SKIP",
	OnbEnlistNowBtn:     "PRESS ANY BUTTON",
	OnbStepThemeTitle:   "HUD THEME",
	OnbStepTriggerTitle: "SUMMON KEY",
	OnbStepLoadoutTitle: "LOADOUT",
	OnbStepDrillTitle:   "CALL IN RESUPPLY",
	OnbRecBadge:         "REC",
	OnbTrigDouble:       "Double-Tap Hold Ctrl",
	OnbTrigLeft:         "Hold Left Ctrl",
	OnbTrigRight:        "Hold Right Ctrl",
	OnbPodCloseWin:      "Close Window",
	OnbPodFiles:         "File Explorer",
	OnbPodSplit:         "Tile Left / Right",
	OnbPodBrowser:       "Browser",
	OnbPodScreenshot:    "Screenshot",
	OnbPodLock:          "Lock Screen",
	OnbDrillReady:       "✓ READY",
	OnbBackBtn:          "BACK",
	OnbNextBtn:          "NEXT",
	OnbFinishDiveBtn:    "DEPLOY",
}

// For returns the localized string table for lang (defaulting to Chinese).
func For(lang catalog.Language) Strings {
	if lang == catalog.LangEN {
		return enStrings
	}
	return zhStrings
}

// FormatError translates Catalog ConflictError and ValidationError into lang.
func FormatError(err error, lang catalog.Language) string {
	if err == nil {
		return ""
	}
	if lang != catalog.LangEN {
		return err.Error()
	}

	var ce *catalog.ConflictError
	if errors.As(err, &ce) {
		return fmt.Sprintf("Sequence conflict: %q overlaps %q on [%s]", ce.FirstName, ce.SecondName, ce.PrefixArrows())
	}

	var ve *catalog.ValidationError
	if errors.As(err, &ve) {
		switch ve.Code {
		case catalog.ValEmptyName:
			return "Stratagem name is required"
		case catalog.ValCodeLength:
			return "Sequence must be 1–12 steps"
		case catalog.ValEmptyStepTarget:
			return "Action target is required"
		case catalog.ValDuplicateID:
			return "Duplicate stratagem ID"
		case catalog.ValInvalidTriggerMode:
			return "Invalid summon shortcut"
		case catalog.ValInvalidHUDTheme:
			return "Invalid HUD theme"
		case catalog.ValInvalidLanguage:
			return "Invalid language"
		case catalog.ValSystemProtected:
			return "System stratagem 'Join the Helldivers' is protected by the Ministry of Truth and cannot be removed or modified"
		default:
			if ve.Reason != "" {
				return ve.Reason
			}
		}
	}
	return err.Error()
}
