import type { Colleague } from '../types';

export const colleagues: Colleague[] = [
  {
    id: 'xiaodi',
    name: '小迪',
    role: '你的办公同事',
    description: '擅长通知、纪要、周报和邮件草稿。',
    strengths: ['通知', '纪要', '周报', '邮件'],
    tasks: ['写通知', '会议纪要', '周报总结', '邮件草稿'],
  },
  {
    id: 'aning',
    name: '阿宁',
    role: '你的数据同事',
    description: '擅长表格整理、数据汇总和分析摘要。',
    strengths: ['表格整理', '数据汇总', '图表分析'],
    tasks: ['整理表格', '汇总数据', '生成图表', '写分析摘要'],
  },
  {
    id: 'laochen',
    name: '老陈',
    role: '你的生产同事',
    description: '擅长日报、交接班和异常说明。',
    strengths: ['生产日报', '交接班', '异常汇总'],
    tasks: ['生产日报', '交接班记录', '异常说明', '上报摘要'],
  },
  {
    id: 'xiaozhou',
    name: '小周',
    role: '你的质量同事',
    description: '擅长问题归类、原因分析和整改建议。',
    strengths: ['质量说明', '原因分析', '整改建议'],
    tasks: ['质量说明', '问题归类', '整改建议', '原因分析'],
  },
];
