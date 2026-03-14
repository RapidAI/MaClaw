import { colors, remoteCardStyle, remoteMetaLabelStyle } from "./styles";

type StatusCard = {
    label: string;
    value: string;
    tone: string;
    detail: string;
};

type Props = {
    cards: StatusCard[];
};

export function RemoteStatusCards({ cards }: Props) {
    return (
        <div style={{ display: 'grid', gridTemplateColumns: 'repeat(auto-fit, minmax(210px, 1fr))', gap: '10px', marginBottom: '14px' }}>
            {cards.map((item) => (
                <div key={item.label} style={remoteCardStyle}>
                    <div style={remoteMetaLabelStyle}>{item.label}</div>
                    <div style={{ fontSize: '1rem', fontWeight: 700, color: item.tone, marginBottom: '6px' }}>{item.value}</div>
                    <div style={{ fontSize: '0.76rem', color: colors.textSecondary, wordBreak: 'break-word' }}>{item.detail}</div>
                </div>
            ))}
        </div>
    );
}
