var tags = ["holiday", "camping", "school", "uni", "Keele", "Salford", "Sheffield", "Sunny Beach", "Huddersfield"];
var people = ["Jem", "Rob", "Josh", "Rubs", "Bez", "Loz", "Harry", "Dale", "Malta Josh", "Starky", "Sam"];

$(".upload-result-container").each(function(i) {
    $(this).find(".description-input").val("Description no. " + (i+1).toString());
    $(this).find(".tags-input").tokenfield('setTokens', randomMultiple(tags));
    $(this).find(".people-input").tokenfield('setTokens', randomMultiple(people));
    setTimeout(function() {
        $(this).find(".btn-primary").trigger("click");
    }, getRandomInt(750));
});

function randomMultiple(arr) {
    var result = [];
    var count = getRandomInt(3);
    for (var i = 0; i < count; i++) {
        result[i] = arr[getRandomInt(arr.length) - 1];
    }
    return result;
}

function getRandomInt(max) {
    // 1 to max (inclusive)
    return Math.floor(Math.random() * Math.floor(max)) + 1;
}