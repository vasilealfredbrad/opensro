/*
===========================================================================

devilspirit.go - the Devil's Spirit, imported from a later client

Port-only, not native (owner decision 2026-10-18). The Devil's Spirit
(iSRO codename family ITEM_MALL_AVATAR_[M|W]_NASRUN) arrived in a client
long after v1.150; there it is a bracelet in its own avatar socket
(TID 3.1.14.1) that, once awakened, raises its wearer's HP and MP, and
at higher enhancement levels the block rate, and grants the devil skills.
v1.150 has no such socket, so the port maps it onto the avatar
attachment (TID 3.1.13.3) and carries the later rules over:

  - the rows: the later client's ids (24385..24392, above every v1.150
    item id) and codenames, cloned from the v1.150 Halloween attachment of
    the same sex, so every requirement and native field is a v1.150 value
    (enterworld.AddPortOnlyItems);

  - the options: the later client's MATTR_NASRUN_* magicoption rows
    (UMDU damage against unique monsters, HPNA / MPNA max HP / MP percent,
    BLOCKRATE block rate) join the v1.150 table
    (enterworld.AddPortOnlyMagicOptions); combat gives them their keeper
    writes. Their values are the later client's per-row, per-level
    _RefMagicOptByItemOptLevel rows (devilspirit.json "levels");

  - the awakening, the timed enhancement and the options derived from
    them: state.go;

  - the Awakening Enhancement Scroll and the extension item: two
    port-only ETC rows cloned from the v1.150 armour gender change tool,
    whose drag-onto-an-item use (0x75BD with a target bag slot) is
    exactly theirs (action/devilspirit_use.go);

  - the look: the awakened devil drawn on the wearer, built by the asset
    pipeline from the owner's import (scripts/build/char/devilSpirit.mjs);

  - the socket: in the later client the Devil's Spirit is worn on its
    own, with or without an avatar dress. Mapped onto the attachment it
    would inherit the v1.150 rule that an attachment needs a worn dress
    (action/avatartransfer.go), so a player without a dress could not
    wear it at all. Is exempts these rows from that rule (and from the
    dress removal that sends the attachment back to the bag); the
    client's equip gate reads the same ids.

devilspirit.json is the one data file the server and the asset build
both read; scripts/test/assets/devilSpirit.test.mjs checks its tables
against the owner's import of the later client.

With SRO_DEVIL_SPIRIT off, the default here, no row is added and the
game is native; the GameWorld's Nomad job turns it on.

===========================================================================
*/
package devilspirit

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"opensro.online/server/internal/game/enterworld"
)

// EnvDevilSpirit turns the Devil's Spirit rows on.
const EnvDevilSpirit = "SRO_DEVIL_SPIRIT"

const (
	// dataFormat is devilspirit.json's format marker.
	dataFormat = "opensro-devil-spirit-v1"
	// firstPortOnlyID is the lowest id a row may take: every v1.150
	// itemdata id is below it (the last is 24316).
	firstPortOnlyID = 24317
	// firstPortOnlyOption is the lowest magic option param id a row may
	// take: every v1.150 magicoption.txt id is at most 256.
	firstPortOnlyOption = 257
	// levelCount is the ability tables' OptLevel 0..MaxLevel.
	levelCount = MaxLevel + 1
	// ratioTotal is the scroll ratios' sum (the original draws rand()%101).
	ratioTotal = 100
	// airdropChance is one Devil's Spirit in this many airdrop rolls.
	airdropChance = 40
	// airdropUniqueChance is one S grade in this many Devil's Spirits.
	airdropUniqueChance = 8
)

// The option tags (magicoption.txt column 7, packed ASCII).
const (
	tagUMDU = "umdu"
	tagHPNA = "hpna"
	tagMPNA = "mpna"
	tagBRNA = "brna"
)

// The ETC kinds.
const (
	kindUpgrade   = "upgrade"
	kindExtension = "extension"
)

//go:embed devilspirit.json
var dataJSON []byte

/*
================
Level

One effective OptLevel of a row: max HP and MP percent, block rate and
the skills it grants.
================
*/
type Level struct {
	HP     uint32   `json:"hp"`
	MP     uint32   `json:"mp"`
	Block  uint32   `json:"block"`
	Skills []uint32 `json:"skills"`
}

/*
================
Item

One Devil's Spirit row of devilspirit.json.
================
*/
type Item struct {
	ID       uint32   `json:"id"`
	Codename string   `json:"codename"`
	Sex      string   `json:"sex"`
	Grade    string   `json:"grade"`
	Name     string   `json:"name"`
	Icon     string   `json:"icon"`
	Models   []string `json:"models"`
	Levels   []Level  `json:"levels"`
}

/*
================
Etc

The Awakening Enhancement Scroll or the extension item. Seconds is the
later row's Param1: the binding's duration, or the period an extension
adds. Description is the tooltip text the asset build files under
EtcDescriptionSymbol (the later client's own text names the later
client's controls).
================
*/
type Etc struct {
	ID          uint32 `json:"id"`
	Codename    string `json:"codename"`
	Kind        string `json:"kind"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Icon        string `json:"icon"`
	Seconds     uint32 `json:"seconds"`
}

/*
================
Skill

A later-client skilldata row: its 118 columns, those not "0" given by
index (the Korean name column left out). Name and Description are the
later client's English text for its name symbols (column 62 and 64),
which the asset build adds to the client's text catalogue.
================
*/
type Skill struct {
	ID          uint32            `json:"id"`
	Codename    string            `json:"codename"`
	Name        string            `json:"name"`
	Description string            `json:"description"`
	Columns     map[string]string `json:"columns"`
}

/*
================
MagicOption

A later-client magicoption row.
================
*/
type MagicOption struct {
	ID         uint32    `json:"id"`
	Codename   string    `json:"codename"`
	Tag        string    `json:"tag"`
	RangeWords [3]uint32 `json:"rangeWords"`
}

/*
================
Data

devilspirit.json.
================
*/
type Data struct {
	Format        string            `json:"format"`
	NativeType    [4]int64          `json:"nativeType"`
	PortType      [4]int64          `json:"portType"`
	Templates     map[string]string `json:"templates"`
	EtcTemplate   string            `json:"etcTemplate"`
	MagicOptions  []MagicOption     `json:"magicOptions"`
	UpgradeRatios []uint32          `json:"upgradeRatios"`
	AwakeSeconds  uint32            `json:"awakeSeconds"`
	Etc           []Etc             `json:"etc"`
	Items         []Item            `json:"items"`
	Skills        []Skill           `json:"skills"`
}

// data is the validated embedded table; a bad row is a build defect.
var data = mustParse(dataJSON)

// rowIDs is the set of data's Devil's Spirit row ids (Is).
var rowIDs = idSet(data.Items)

/*
================
mustParse
================
*/
func mustParse(raw []byte) Data {
	parsed, err := parse(raw)
	if err != nil {
		panic(err)
	}
	return parsed
}

/*
================
parse

Validates devilspirit.json: unique ids above the v1.150 range, unique
codenames, a template per sex, the avatar attachment type, a model list
and eleven ability levels for every row, the four option rows, the
scroll's ratios and the two ETC rows.
================
*/
func parse(raw []byte) (Data, error) {
	var d Data
	if err := json.Unmarshal(raw, &d); err != nil {
		return Data{}, fmt.Errorf("devilspirit: %w", err)
	}
	if d.Format != dataFormat {
		return Data{}, fmt.Errorf("devilspirit: format %q", d.Format)
	}
	if d.PortType != [4]int64{3, 1, 13, 3} {
		return Data{}, fmt.Errorf("devilspirit: port type %v is not the avatar attachment", d.PortType)
	}
	ids, codenames := map[uint32]bool{}, map[string]bool{}
	identity := func(id uint32, codename string) error {
		switch {
		case id < firstPortOnlyID || ids[id]:
			return fmt.Errorf("devilspirit: id %d is a v1.150 id or repeated", id)
		case codename == "" || codenames[codename]:
			return fmt.Errorf("devilspirit: codename %q is empty or repeated", codename)
		}
		ids[id], codenames[codename] = true, true
		return nil
	}
	for _, item := range d.Items {
		if err := identity(item.ID, item.Codename); err != nil {
			return Data{}, err
		}
		switch {
		case d.Templates[item.Sex] == "":
			return Data{}, fmt.Errorf("devilspirit: %s has no template for sex %q", item.Codename, item.Sex)
		case item.Name == "" || item.Icon == "" || len(item.Models) == 0:
			return Data{}, fmt.Errorf("devilspirit: %s lacks a name, icon or model", item.Codename)
		case len(item.Levels) != levelCount:
			return Data{}, fmt.Errorf("devilspirit: %s has %d levels, want %d", item.Codename, len(item.Levels), levelCount)
		}
		for _, level := range item.Levels {
			if level.HP == 0 || level.MP == 0 || len(level.Skills) == 0 {
				return Data{}, fmt.Errorf("devilspirit: %s has an empty level", item.Codename)
			}
		}
	}
	if len(d.Items) == 0 {
		return Data{}, fmt.Errorf("devilspirit: no rows")
	}
	kinds := map[string]bool{}
	for _, etc := range d.Etc {
		if err := identity(etc.ID, etc.Codename); err != nil {
			return Data{}, err
		}
		if etc.Kind != kindUpgrade && etc.Kind != kindExtension || kinds[etc.Kind] || etc.Seconds == 0 || etc.Name == "" ||
			etc.Description == "" || etc.Icon == "" {
			return Data{}, fmt.Errorf("devilspirit: ETC row %s", etc.Codename)
		}
		kinds[etc.Kind] = true
	}
	if len(kinds) != 2 || d.EtcTemplate == "" {
		return Data{}, fmt.Errorf("devilspirit: the scroll and extension rows or their template are missing")
	}
	tags, options := map[string]bool{}, map[uint32]bool{}
	for _, option := range d.MagicOptions {
		if option.ID < firstPortOnlyOption || option.ID > 0xffff || options[option.ID] || len(option.Tag) != 4 || tags[option.Tag] || option.Codename == "" {
			return Data{}, fmt.Errorf("devilspirit: magic option %d %q", option.ID, option.Codename)
		}
		tags[option.Tag], options[option.ID] = true, true
	}
	for _, tag := range []string{tagUMDU, tagHPNA, tagMPNA, tagBRNA} {
		if !tags[tag] {
			return Data{}, fmt.Errorf("devilspirit: no %s magic option", tag)
		}
	}
	skills := map[uint32]bool{}
	for _, skill := range d.Skills {
		if skill.ID == 0 || skills[skill.ID] || skill.Codename == "" || skill.Name == "" {
			return Data{}, fmt.Errorf("devilspirit: skill %d %q", skill.ID, skill.Codename)
		}
		skills[skill.ID] = true
	}
	for _, item := range d.Items {
		for _, level := range item.Levels {
			for _, id := range level.Skills {
				if !skills[id] {
					return Data{}, fmt.Errorf("devilspirit: %s grants skill %d the table lacks", item.Codename, id)
				}
			}
		}
	}
	var total uint32
	for _, ratio := range d.UpgradeRatios {
		total += ratio
	}
	if total != ratioTotal || len(d.UpgradeRatios) != levelCount || d.AwakeSeconds == 0 {
		return Data{}, fmt.Errorf("devilspirit: upgrade ratios %v or awakening %d", d.UpgradeRatios, d.AwakeSeconds)
	}
	return d, nil
}

/*
================
option

The magic option with tag (parse proved each exists).
================
*/
func (d Data) option(tag string) MagicOption {
	for _, option := range d.MagicOptions {
		if option.Tag == tag {
			return option
		}
	}
	panic("devilspirit: no " + tag + " option")
}

/*
================
etc

The ETC row of kind (parse proved each exists).
================
*/
func (d Data) etc(kind string) Etc {
	for _, row := range d.Etc {
		if row.Kind == kind {
			return row
		}
	}
	panic("devilspirit: no " + kind + " row")
}

/*
================
idSet
================
*/
func idSet(items []Item) map[uint32]bool {
	set := make(map[uint32]bool, len(items))
	for _, item := range items {
		set[item.ID] = true
	}
	return set
}

/*
================
Is

Whether refObjID is a Devil's Spirit row. Its ids lie above every v1.150
item id, so with SRO_DEVIL_SPIRIT off no item can carry one and the
native rules it relaxes stay whole.
================
*/
func Is(refObjID uint32) bool {
	return rowIDs[refObjID]
}

/*
================
IsUpgradeScroll
================
*/
func IsUpgradeScroll(refObjID uint32) bool {
	return refObjID == data.etc(kindUpgrade).ID
}

/*
================
IsExtension
================
*/
func IsExtension(refObjID uint32) bool {
	return refObjID == data.etc(kindExtension).ID
}

/*
================
UpgradeSeconds

How long a scroll's binding lasts (the later row's Param1, 3 hours).
================
*/
func UpgradeSeconds() uint32 {
	return data.etc(kindUpgrade).Seconds
}

/*
================
ExtensionSeconds

How long an extension adds (the later row's Param1, 28 days).
================
*/
func ExtensionSeconds() uint32 {
	return data.etc(kindExtension).Seconds
}

/*
================
EtcDescriptionSymbol

The text symbol of a scroll or extension row's description.
================
*/
func EtcDescriptionSymbol(codename string) string {
	return "SN_" + codename + "_TT_DESC"
}

/*
================
Items

A copy of the Devil's Spirit rows.
================
*/
func Items() []Item {
	return append([]Item(nil), data.Items...)
}

/*
================
EtcItems

A copy of the scroll and extension rows.
================
*/
func EtcItems() []Etc {
	return append([]Etc(nil), data.Etc...)
}

/*
================
FromEnv

SRO_DEVIL_SPIRIT: "on", "1" or "true" adds the rows; anything else is
native.
================
*/
func FromEnv() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv(EnvDevilSpirit))) {
	case "on", "1", "true":
		return true
	}
	return false
}

/*
================
packTag

A four-letter tag as magicoption.txt column 7 packs it.
================
*/
func packTag(tag string) uint32 {
	var packed uint32
	for i := 0; i < len(tag); i++ {
		packed = packed<<8 | uint32(tag[i])
	}
	return packed
}

/*
================
MagicOptionRows

The later client's MATTR_NASRUN_* definitions as v1.150 catalogue rows
(degree 1, visible "+" parameter).
================
*/
func MagicOptionRows() []enterworld.MagicOptionRow {
	rows := make([]enterworld.MagicOptionRow, 0, len(data.MagicOptions))
	for _, option := range data.MagicOptions {
		words := option.RangeWords
		rows = append(rows, enterworld.MagicOptionRow{
			RangeWords: &words,
			ParamID:    option.ID,
			OptionName: option.Codename,
			ParamName:  "+",
			Degree:     1,
			Tag:        packTag(option.Tag),
		})
	}
	return rows
}

/*
================
MagicOptionTarget

The magic option catalogue Apply extends.
================
*/
type MagicOptionTarget interface {
	AddPortOnlyMagicOptions(rows []enterworld.MagicOptionRow) (int, error)
}

/*
================
Apply

Adds the MATTR_NASRUN_* option rows, the devil skills (skills.go),
every Devil's Spirit row (each made with its level-0 options, the
template a new instance starts from) and the scroll and extension rows.
================
*/
func Apply(items *enterworld.TextdataItems, options MagicOptionTarget, skills SkillTarget) (int, error) {
	if _, err := options.AddPortOnlyMagicOptions(MagicOptionRows()); err != nil {
		return 0, err
	}
	skillRows, err := SkillRows()
	if err != nil {
		return 0, err
	}
	if _, err := skills.AddPortOnlySkillRows(skillRows); err != nil {
		return 0, err
	}
	rows := make([]enterworld.PortOnlyItem, 0, len(data.Items)+len(data.Etc))
	for _, item := range data.Items {
		rows = append(rows, enterworld.PortOnlyItem{
			RefObjID:     item.ID,
			Codename:     item.Codename,
			Template:     data.Templates[item.Sex],
			NameStrID:    "SN_" + item.Codename,
			Name:         item.Name,
			Icon:         item.Icon,
			MagicOptions: Options(item.ID, 0, 0),
		})
	}
	for _, etc := range data.Etc {
		rows = append(rows, enterworld.PortOnlyItem{
			RefObjID:          etc.ID,
			Codename:          etc.Codename,
			Template:          data.EtcTemplate,
			NameStrID:         "SN_" + etc.Codename,
			Name:              etc.Name,
			DescriptionSymbol: EtcDescriptionSymbol(etc.Codename),
			Icon:              etc.Icon,
		})
	}
	return items.AddPortOnlyItems(rows)
}

/*
================
ItemSource

The item rows by codename (enterworld.ItemRefSource's lookup).
================
*/
type ItemSource interface {
	ItemRefByCodename(codename string) (*enterworld.ItemRef, bool)
}

/*
================
Loaded

Whether items holds every Devil's Spirit row (Apply ran: SRO_DEVIL_SPIRIT
is on in the GameWorld's wiring), each with its options. The game asks it
before offering the item anywhere, so with the flag off nothing rolls for
it and the game stays as it was.
================
*/
func Loaded(items ItemSource) bool {
	if items == nil {
		return false
	}
	for _, item := range data.Items {
		ref, ok := items.ItemRefByCodename(item.Codename)
		if !ok || ref == nil || ref.RefObjID != item.ID || len(ref.PortOnlyMagicOptions) == 0 {
			return false
		}
	}
	return true
}

/*
================
AirdropReward

The Devil's Spirit a battle royale airdrop may hold for a wearer of the
given sex: one airdrop roll in airdropChance, and among those one in
airdropUniqueChance is the S grade; the others take the crimson, golden
or blue A grade evenly. ok is false when the roll misses. roll is the
match's random source.
================
*/
func AirdropReward(male bool, roll func() (uint32, error)) (codename string, ok bool, err error) {
	n, err := roll()
	if err != nil || n%airdropChance != 0 {
		return "", false, err
	}
	pick, err := roll()
	if err != nil {
		return "", false, err
	}
	grade := [...]string{"red", "yellow", "blue"}[pick%3]
	if pick%airdropUniqueChance == 0 {
		grade = "unique"
	}
	sex := "W"
	if male {
		sex = "M"
	}
	for _, item := range data.Items {
		if item.Sex == sex && item.Grade == grade {
			return item.Codename, true, nil
		}
	}
	return "", false, fmt.Errorf("devilspirit: no %s %s row", sex, grade)
}
