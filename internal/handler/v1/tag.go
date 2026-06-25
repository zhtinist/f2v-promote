package v1

import (
	"net/http"
	"strconv"

	"github.com/AiMarketool/f2v-promote/internal/model"
	"github.com/AiMarketool/f2v-promote/internal/repository"
	"github.com/gin-gonic/gin"
)

// TagHandler 标签管理
type TagHandler struct {
	tagRepo *repository.ZhugeTagRepo
}

func NewTagHandler(tagRepo *repository.ZhugeTagRepo) *TagHandler {
	return &TagHandler{tagRepo: tagRepo}
}

// tagTreeNode 树形结构节点
type tagTreeNode struct {
	model.ZhugeTag
	Children []model.ZhugeTag `json:"children"`
}

// List 标签列表（分页 + 筛选）
func (h *TagHandler) List(c *gin.Context) {
	keyword := c.Query("keyword")
	level := c.Query("level")
	parentID := c.Query("parent_id")
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", "50"))
	if page < 1 {
		page = 1
	}
	if pageSize < 1 || pageSize > 200 {
		pageSize = 50
	}

	tags, total, err := h.tagRepo.ListPaged(keyword, level, parentID, page, pageSize)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "查询失败"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"list": tags, "total": total, "page": page, "page_size": pageSize})
}

// Tree 标签树形结构
func (h *TagHandler) Tree(c *gin.Context) {
	all, err := h.tagRepo.GetAll()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "查询失败"})
		return
	}

	// 构建树：一级 (parent_id IS NULL) → 二级
	parentMap := make(map[string][]model.ZhugeTag)
	var roots []model.ZhugeTag
	for _, t := range all {
		if t.ParentID == nil || *t.ParentID == "" {
			roots = append(roots, t)
		} else {
			parentMap[*t.ParentID] = append(parentMap[*t.ParentID], t)
		}
	}

	tree := make([]tagTreeNode, 0, len(roots))
	for _, r := range roots {
		node := tagTreeNode{ZhugeTag: r, Children: parentMap[r.ID]}
		if node.Children == nil {
			node.Children = []model.ZhugeTag{}
		}
		tree = append(tree, node)
	}

	c.JSON(http.StatusOK, gin.H{"tree": tree, "total": len(all)})
}

// Sync 手动触发标签同步（调用 weixin client 的标签同步逻辑）
// 注：同步逻辑绑定在 weixinClient 内部，这里通过 repo 暴露
func (h *TagHandler) Sync(c *gin.Context) {
	// 标签同步需要 weixinClient，暂返回提示
	c.JSON(http.StatusOK, gin.H{"message": "请通过创建投放页面触发标签同步"})
}
