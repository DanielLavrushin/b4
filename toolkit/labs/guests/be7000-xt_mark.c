#include <linux/module.h>
#include <linux/skbuff.h>
#include <linux/netfilter/x_tables.h>

MODULE_LICENSE("GPL");
MODULE_DESCRIPTION("xt_mark with the MARK target of the Xiaomi RC06 firmware, which carries a return flag");
MODULE_ALIAS("ipt_mark");
MODULE_ALIAS("ip6t_mark");
MODULE_ALIAS("ipt_MARK");
MODULE_ALIAS("ip6t_MARK");
MODULE_ALIAS("arpt_MARK");

struct rc06_mark_tginfo {
	__u32 mark, mask;
	__u8 ret;
};

struct rc06_mark_mtinfo {
	__u32 mark, mask;
	__u8 invert;
};

static unsigned int rc06_mark_tg(struct sk_buff *skb, const struct xt_action_param *par)
{
	const struct rc06_mark_tginfo *info = par->targinfo;

	skb->mark = (skb->mark & ~info->mask) ^ info->mark;
	return info->ret ? XT_RETURN : XT_CONTINUE;
}

static bool rc06_mark_mt(const struct sk_buff *skb, struct xt_action_param *par)
{
	const struct rc06_mark_mtinfo *info = par->matchinfo;

	return ((skb->mark & info->mask) == info->mark) ^ info->invert;
}

static struct xt_target rc06_mark_tg_reg __read_mostly = {
	.name = "MARK",
	.revision = 2,
	.family = NFPROTO_UNSPEC,
	.target = rc06_mark_tg,
	.targetsize = sizeof(struct rc06_mark_tginfo),
	.me = THIS_MODULE,
};

static struct xt_match rc06_mark_mt_reg __read_mostly = {
	.name = "mark",
	.revision = 1,
	.family = NFPROTO_UNSPEC,
	.match = rc06_mark_mt,
	.matchsize = sizeof(struct rc06_mark_mtinfo),
	.me = THIS_MODULE,
};

static int __init rc06_mark_init(void)
{
	int ret = xt_register_target(&rc06_mark_tg_reg);

	if (ret < 0)
		return ret;
	ret = xt_register_match(&rc06_mark_mt_reg);
	if (ret < 0)
		xt_unregister_target(&rc06_mark_tg_reg);
	return ret;
}

static void __exit rc06_mark_exit(void)
{
	xt_unregister_match(&rc06_mark_mt_reg);
	xt_unregister_target(&rc06_mark_tg_reg);
}

module_init(rc06_mark_init);
module_exit(rc06_mark_exit);
