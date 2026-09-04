// Bento 卡片容器：强制 rounded-2xl + 边框 + 悬停上浮，支持跨网格尺寸。
// 默认带 group，使卡片内图标可通过 group-hover 联动（规范 §1.6）。
export default function BentoCard({ className = '', span = '', children, as: Tag = 'div', ...rest }) {
  const spanClass = span === 'lg' ? 'bento-lg' : span === 'wide' ? 'bento-wide' : span === 'tall' ? 'bento-tall' : ''
  return (
    <Tag className={`bento-card group p-4 md:p-6 ${spanClass} ${className}`} {...rest}>
      {children}
    </Tag>
  )
}
